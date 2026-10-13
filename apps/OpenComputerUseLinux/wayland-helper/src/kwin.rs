//! KDE Plasma (KWin 6): window identity from a KWin script, per-window capture
//! through ScreenShot2, and input through org_kde_kwin_fake_input. Both
//! restricted interfaces are authorized by a .desktop file for this executable
//! (open-computer-use install-kde-integration). Input is checked against the
//! target right before it is sent: the cursor must have reached the point and the
//! target must be the topmost window there, or the active window for keys. See
//! docs/design-docs/linux-display-backends.md ("KDE Plasma").

use std::{
    collections::HashMap,
    io::Read,
    os::{
        fd::{AsFd, FromRawFd, OwnedFd},
        unix::fs::OpenOptionsExt,
    },
    sync::{
        Mutex, OnceLock,
        atomic::{AtomicU64, Ordering},
        mpsc,
    },
    thread,
    time::{Duration, Instant},
};

use serde_json::Value;
use wayland_client::{
    Connection as WaylandConnection, Dispatch, EventQueue, QueueHandle, WEnum,
    globals::{GlobalListContents, registry_queue_init},
    protocol::{wl_keyboard, wl_pointer, wl_registry, wl_seat},
};
use wayland_protocols_plasma::fake_input::client::org_kde_kwin_fake_input::OrgKdeKwinFakeInput;
use zbus::{
    blocking::{Connection, connection::Builder, fdo::DBusProxy},
    zvariant,
};

use crate::{keyboard::Failure, keymap::Keymap};

const KWIN: &str = "org.kde.KWin";
const RECEIVER_PATH: &str = "/com/cherryai/ComputerUse/KWinReport";
const RECEIVER_INTERFACE: &str = "com.cherryai.ComputerUse.KWinReport1";
/// Time clients get to see pointer motion before a press, and the longest a
/// motion may take to reach the cursor.
const SETTLE: Duration = Duration::from_millis(40);
const MOTION_DEADLINE: Duration = Duration::from_secs(1);
const CLICK_GAP: Duration = Duration::from_millis(30);
const SHIFT_L: u32 = 0xffe1;

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

fn not_installed(detail: &str) -> Failure {
    failure(
        "DEPENDENCY_MISSING",
        format!(
            "KDE integration is not active for this helper ({detail}); run `open-computer-use install-kde-integration`"
        ),
    )
}

/// Whether this is a KDE session with KWin on the bus.
pub fn running() -> bool {
    let kde = std::env::var("XDG_CURRENT_DESKTOP")
        .unwrap_or_default()
        .split(':')
        .any(|desktop| desktop.eq_ignore_ascii_case("KDE"));
    kde && connection()
        .ok()
        .and_then(|c| {
            let _runtime = runtime().ok()?.enter();
            DBusProxy::new(c).ok()?.name_has_owner(KWIN.try_into().ok()?).ok()
        })
        .unwrap_or(false)
}

// ---- KWin script queries ----

/// Reports the KWin script sends back, keyed by token, each with the bus name
/// that may send it (KWin's unique name).
type Pending = Mutex<HashMap<String, (String, mpsc::Sender<String>)>>;

struct Receiver {
    pending: &'static Pending,
}

#[zbus::interface(name = "com.cherryai.ComputerUse.KWinReport1")]
impl Receiver {
    fn report(&self, #[zbus(header)] header: zbus::message::Header<'_>, token: String, json: String) {
        let sender = header.sender().map(|s| s.to_string()).unwrap_or_default();
        let mut pending = self.pending.lock().unwrap();
        // KWin's scripting interface is open to any process; only KWin itself,
        // with the token of a query in flight, is believed.
        if pending.get(&token).is_some_and(|(owner, _)| *owner == sender) {
            let (_, reply) = pending.remove(&token).unwrap();
            let _ = reply.send(json);
        }
    }
}

fn pending() -> &'static Pending {
    static PENDING: OnceLock<Pending> = OnceLock::new();
    PENDING.get_or_init(Default::default)
}

/// zbus runs on tokio here (ashpd enables the feature for the whole build), and
/// serving the receiver spawns tasks: a one-worker runtime keeps them running
/// while the helper's thread waits for a report.
fn runtime() -> Result<&'static tokio::runtime::Runtime, Failure> {
    static RUNTIME: OnceLock<tokio::runtime::Runtime> = OnceLock::new();
    if let Some(runtime) = RUNTIME.get() {
        return Ok(runtime);
    }
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(1)
        .enable_all()
        .build()
        .map_err(|e| failure("DEPENDENCY_MISSING", format!("Cannot start the D-Bus runtime: {e}")))?;
    Ok(RUNTIME.get_or_init(|| runtime))
}

fn connection() -> Result<&'static Connection, Failure> {
    static CONNECTION: OnceLock<Connection> = OnceLock::new();
    if let Some(connection) = CONNECTION.get() {
        return Ok(connection);
    }
    let _runtime = runtime()?.enter();
    let connection = Builder::session()
        .and_then(|b| b.method_timeout(Duration::from_secs(10)).build())
        .map_err(|e| failure("DEPENDENCY_MISSING", format!("Cannot reach the session bus: {e}")))?;
    connection
        .object_server()
        .at(RECEIVER_PATH, Receiver { pending: pending() })
        .map_err(|e| failure("DEPENDENCY_MISSING", format!("Cannot receive KWin script reports: {e}")))?;
    Ok(CONNECTION.get_or_init(|| connection))
}

fn token() -> String {
    let mut bytes = [0u8; 16];
    if std::fs::File::open("/dev/urandom").and_then(|mut f| f.read_exact(&mut bytes)).is_err() {
        // Uniqueness still holds; the sender check is what keeps reports honest.
        bytes[..8].copy_from_slice(&Instant::now().elapsed().as_nanos().to_le_bytes()[..8]);
    }
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

/// Runs a KWin script that reports one JSON value, and returns it. The script
/// gets `report(value)`.
fn run_script(body: &str) -> Result<Value, Failure> {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    let connection = connection()?;
    let _runtime = runtime()?.enter();
    let unavailable = |e: zbus::Error| failure("TARGET_UNAVAILABLE", format!("KWin scripting failed: {e}"));
    let owner = DBusProxy::new(connection)
        .map_err(|e| failure("TARGET_UNAVAILABLE", format!("KWin is not running: {e}")))?
        .get_name_owner(KWIN.try_into().unwrap())
        .map_err(|e| failure("TARGET_UNAVAILABLE", format!("KWin is not running: {e}")))?
        .to_string();
    let unique = connection.unique_name().map(|n| n.to_string()).unwrap_or_default();
    let token = token();
    let script = format!(
        "(function () {{ const report = value => callDBus({unique:?}, {RECEIVER_PATH:?}, {RECEIVER_INTERFACE:?}, \"Report\", {token:?}, JSON.stringify(value));\n{body}\n}})();\n"
    );
    let name = format!("ocu-{}-{}", std::process::id(), COUNTER.fetch_add(1, Ordering::Relaxed));
    let dir = std::env::var("XDG_RUNTIME_DIR").unwrap_or_else(|_| std::env::temp_dir().display().to_string());
    let path = format!("{dir}/{name}.js");
    std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&path)
        .and_then(|mut file| std::io::Write::write_all(&mut file, script.as_bytes()))
        .map_err(|e| failure("TARGET_UNAVAILABLE", format!("Cannot write the KWin script: {e}")))?;
    let (send, receive) = mpsc::channel();
    pending().lock().unwrap().insert(token.clone(), (owner, send));
    let result = (|| {
        let (id,): (i32,) = connection
            .call_method(Some(KWIN), "/Scripting", Some("org.kde.kwin.Scripting"), "loadScript", &(&path, &name))
            .and_then(|reply| reply.body().deserialize())
            .map_err(unavailable)?;
        if id < 0 {
            return Err(failure("TARGET_UNAVAILABLE", "KWin did not load the script"));
        }
        connection
            .call_method(Some(KWIN), format!("/Scripting/Script{id}").as_str(), Some("org.kde.kwin.Script"), "run", &())
            .map_err(unavailable)?;
        let json = receive
            .recv_timeout(Duration::from_secs(3))
            .map_err(|_| failure("TARGET_UNAVAILABLE", "The KWin script did not report back"))?;
        serde_json::from_str(&json).map_err(|e| failure("TARGET_UNAVAILABLE", format!("Unreadable KWin report: {e}")))
    })();
    pending().lock().unwrap().remove(&token);
    let _ = connection.call_method(Some(KWIN), "/Scripting", Some("org.kde.kwin.Scripting"), "unloadScript", &(&name,));
    let _ = std::fs::remove_file(&path);
    result
}

/// The windows bottom of the stack first, the active window, the cursor, and the
/// windows at `point` topmost first.
const STATE: &str = r#"
const rect = r => [r.x, r.y, r.width, r.height];
const current = workspace.currentDesktop;
const windows = workspace.stackingOrder.map(w => ({
  id: w.internalId.toString(), pid: w.pid, caption: w.caption,
  frame: rect(w.frameGeometry), buffer: rect(w.bufferGeometry),
  active: w.active, minimized: w.minimized,
  visible: !w.minimized && (w.onAllDesktops || w.desktops.some(d => d === current)),
  normal: w.normalWindow || w.dialog, dock: w.dock, notification: w.notification || w.criticalNotification,
}));
const cursor = workspace.cursorPos;
const at = POINT ? workspace.windowAt(POINT, 1) : [];
report({ windows, active: workspace.activeWindow ? workspace.activeWindow.internalId.toString() : null,
  cursor: [cursor.x, cursor.y], at: at.map(w => ({ id: w.internalId.toString(), caption: w.caption, dock: w.dock,
    notification: w.notification || w.criticalNotification })) });
"#;

/// KWin's view of the session, with the windows at `point` when given.
pub fn state(point: Option<(f64, f64)>) -> Result<Value, Failure> {
    let point = match point {
        Some((x, y)) => format!("{{ x: {x}, y: {y} }}"),
        None => "null".into(),
    };
    run_script(&STATE.replace("POINT", &point))
}

// ---- capture ----

/// Pixels of one window from ScreenShot2: (format, width, height, stride, scale, data).
pub struct Capture {
    pub format: u32,
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub data: Vec<u8>,
}

/// QImage formats in KWin's metadata, mapped to the helper's frame formats:
/// RGB32, ARGB32 and premultiplied ARGB32 are B, G, R, A bytes (wl_shm XRGB8888);
/// RGBX8888 and RGBA8888 variants are R, G, B, A (DRM XBGR8888).
fn frame_format(qimage: u32) -> Option<u32> {
    match qimage {
        4..=6 => Some(1),
        16..=18 => Some(0x3432_4258),
        _ => None,
    }
}

/// Captures one window without its decoration or shadow.
pub fn capture(internal_id: &str) -> Result<Capture, Failure> {
    let connection = connection()?;
    let _runtime = runtime()?.enter();
    let mut fds = [0; 2];
    if unsafe { libc::pipe2(fds.as_mut_ptr(), libc::O_CLOEXEC) } != 0 {
        return Err(failure("CAPTURE_FAILED", "Cannot create a pipe"));
    }
    let (read, write) = unsafe { (OwnedFd::from_raw_fd(fds[0]), OwnedFd::from_raw_fd(fds[1])) };
    let reader = thread::spawn(move || {
        let mut data = Vec::new();
        let _ = std::fs::File::from(read).read_to_end(&mut data);
        data
    });
    let options: HashMap<&str, zvariant::Value> =
        [("include-decoration", false.into()), ("include-shadow", false.into()), ("include-cursor", false.into())]
            .into_iter()
            .collect();
    let reply = connection.call_method(
        Some(KWIN),
        "/org/kde/KWin/ScreenShot2",
        Some("org.kde.KWin.ScreenShot2"),
        "CaptureWindow",
        &(internal_id, options, zvariant::Fd::from(write.as_fd())),
    );
    drop(write);
    let reply = reply.map_err(|e| match &e {
        zbus::Error::MethodError(name, _, _) if name.as_str().ends_with("NoAuthorized") => {
            not_installed("ScreenShot2 refused")
        }
        _ => failure("CAPTURE_FAILED", format!("KWin could not capture the window: {e}")),
    })?;
    let (meta,): (HashMap<String, zvariant::OwnedValue>,) = reply
        .body()
        .deserialize()
        .map_err(|e| failure("CAPTURE_FAILED", format!("Unreadable capture metadata: {e}")))?;
    let number = |key: &str| meta.get(key).and_then(|v| u32::try_from(v).ok());
    let data = reader.join().unwrap_or_default();
    let (Some(qimage), Some(width), Some(height), Some(stride)) =
        (number("format"), number("width"), number("height"), number("stride"))
    else {
        return Err(failure("CAPTURE_FAILED", "KWin returned incomplete capture metadata"));
    };
    let format = frame_format(qimage)
        .ok_or_else(|| failure("CAPTURE_FAILED", format!("Unsupported KWin image format {qimage}")))?;
    if data.len() < stride as usize * height as usize {
        return Err(failure("CAPTURE_FAILED", "KWin returned a short window image"));
    }
    Ok(Capture { format, width, height, stride, data })
}

/// Whether ScreenShot2 serves this helper, asked with a window id that cannot
/// exist: KWin checks the caller first, so only "no such window" means yes.
/// Anything else, such as a timeout, says nothing about the authorization.
/// Otherwise the error says why.
pub fn screenshot_authorized() -> Result<(), String> {
    match capture("{00000000-0000-0000-0000-000000000000}") {
        Ok(_) => Ok(()),
        Err(failure) if failure.message.contains("InvalidWindow") => Ok(()),
        Err(failure) => Err(failure.message),
    }
}

// ---- fake input ----

#[derive(Default)]
struct InputState {
    keymap: Option<Keymap>,
}

impl Dispatch<wl_registry::WlRegistry, GlobalListContents> for InputState {
    fn event(
        _: &mut Self,
        _: &wl_registry::WlRegistry,
        _: wl_registry::Event,
        _: &GlobalListContents,
        _: &WaylandConnection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<wl_seat::WlSeat, ()> for InputState {
    fn event(
        _: &mut Self,
        _: &wl_seat::WlSeat,
        _: wl_seat::Event,
        _: &(),
        _: &WaylandConnection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<wl_keyboard::WlKeyboard, ()> for InputState {
    fn event(
        state: &mut Self,
        _: &wl_keyboard::WlKeyboard,
        event: wl_keyboard::Event,
        _: &(),
        _: &WaylandConnection,
        _: &QueueHandle<Self>,
    ) {
        if let wl_keyboard::Event::Keymap { format: WEnum::Value(wl_keyboard::KeymapFormat::XkbV1), fd, size } = event {
            let mut text = vec![0; size as usize];
            let mut file = std::fs::File::from(fd);
            if file.read_exact(&mut text).is_ok() {
                state.keymap = Keymap::parse(&text).ok();
            }
        }
    }
}
impl Dispatch<OrgKdeKwinFakeInput, ()> for InputState {
    fn event(
        _: &mut Self,
        _: &OrgKdeKwinFakeInput,
        _: <OrgKdeKwinFakeInput as wayland_client::Proxy>::Event,
        _: &(),
        _: &WaylandConnection,
        _: &QueueHandle<Self>,
    ) {
    }
}

/// One fake_input connection, kept for the helper's lifetime: KWin creates an
/// input device per connection, and a new device's first events can be dropped.
pub struct FakeInput {
    _connection: WaylandConnection,
    queue: EventQueue<InputState>,
    state: InputState,
    fake: OrgKdeKwinFakeInput,
    held_buttons: Vec<u32>,
    held_keys: Vec<u32>,
}

impl FakeInput {
    pub fn connect() -> Result<Self, Failure> {
        let unavailable = |e: String| failure("DEPENDENCY_MISSING", format!("Cannot connect to KWin: {e}"));
        let connection = WaylandConnection::connect_to_env().map_err(|e| unavailable(e.to_string()))?;
        let (globals, mut queue) =
            registry_queue_init::<InputState>(&connection).map_err(|e| unavailable(e.to_string()))?;
        let handle = queue.handle();
        // KWin offers fake_input only to clients a .desktop file authorizes.
        let fake: OrgKdeKwinFakeInput =
            globals.bind(&handle, 4..=5, ()).map_err(|_| not_installed("fake_input is not offered"))?;
        fake.authenticate("Cherry Computer Use".into(), "Automating windows the user asked about".into());
        let seat: wl_seat::WlSeat = globals.bind(&handle, 1..=5, ()).map_err(|e| unavailable(e.to_string()))?;
        let _keyboard = seat.get_keyboard(&handle, ());
        let mut state = InputState::default();
        for _ in 0..2 {
            queue.roundtrip(&mut state).map_err(|e| unavailable(e.to_string()))?;
        }
        Ok(FakeInput { _connection: connection, queue, state, fake, held_buttons: Vec::new(), held_keys: Vec::new() })
    }

    fn sync(&mut self) -> Result<(), Failure> {
        self.queue.roundtrip(&mut self.state).map(|_| ()).map_err(|e| Failure {
            code: "TARGET_UNAVAILABLE",
            message: format!("Wayland connection lost: {e}"),
            sent: true,
        })
    }

    fn button(&mut self, code: u32, pressed: bool) -> Result<(), Failure> {
        let state = if pressed { wl_pointer::ButtonState::Pressed } else { wl_pointer::ButtonState::Released };
        self.fake.button(code, u32::from(state));
        if pressed {
            self.held_buttons.push(code);
        } else {
            self.held_buttons.retain(|c| *c != code);
        }
        self.sync()
    }

    fn key(&mut self, evdev: u32, pressed: bool) -> Result<(), Failure> {
        self.fake.keyboard_key(evdev, u32::from(pressed));
        if pressed {
            self.held_keys.push(evdev);
        } else {
            self.held_keys.retain(|c| *c != evdev);
        }
        self.sync()
    }

    /// Evdev codes and Shift needs for keysyms in the first layout.
    fn resolve(&self, keysym: u32) -> Option<(u32, bool)> {
        let key = self.state.keymap.as_ref()?.keysym(keysym, 0)?;
        key.code.checked_sub(8).map(|code| (code, key.shift))
    }

    fn resolve_char(&self, character: char) -> Option<(u32, bool)> {
        let key = self.state.keymap.as_ref()?.character(character, 0)?;
        key.code.checked_sub(8).map(|code| (code, key.shift))
    }
}

impl Drop for FakeInput {
    fn drop(&mut self) {
        for code in std::mem::take(&mut self.held_keys).into_iter().rev() {
            self.fake.keyboard_key(code, 0);
        }
        for code in std::mem::take(&mut self.held_buttons) {
            self.fake.button(code, u32::from(wl_pointer::ButtonState::Released));
        }
        if wayland_client::Proxy::version(&self.fake) >= 5 {
            self.fake.destroy();
        }
        let _ = self.queue.roundtrip(&mut self.state);
    }
}

// ---- targeted input ----

fn windows(state: &Value) -> impl Iterator<Item = &Value> {
    state.get("windows").and_then(Value::as_array).into_iter().flatten()
}

/// The target as KWin lists it now, which must be visible.
fn visible_target(state: &Value, id: &str) -> Result<(), Failure> {
    let window = windows(state)
        .find(|w| w.get("id").and_then(Value::as_str) == Some(id))
        .ok_or_else(|| failure("TARGET_UNAVAILABLE", "The target window is gone"))?;
    if window.get("visible").and_then(Value::as_bool) != Some(true) {
        return Err(failure("TARGET_UNAVAILABLE", "The target window is minimized or on another desktop"));
    }
    Ok(())
}

fn describe_at(state: &Value) -> String {
    match state.get("at").and_then(Value::as_array).and_then(|at| at.first()) {
        None => "nothing".into(),
        Some(w) => {
            let kind = if w.get("notification").and_then(Value::as_bool) == Some(true) {
                "the notification"
            } else if w.get("dock").and_then(Value::as_bool) == Some(true) {
                "the panel"
            } else {
                "the window"
            };
            format!("{kind} \"{}\"", w.get("caption").and_then(Value::as_str).unwrap_or_default())
        }
    }
}

/// Moves the pointer onto a point of the target and confirms, right before a
/// press, that the cursor is there and the target is the topmost window there.
fn point_at(input: &mut FakeInput, id: &str, x: f64, y: f64) -> Result<(), Failure> {
    visible_target(&state(None)?, id)?;
    let deadline = Instant::now() + MOTION_DEADLINE;
    loop {
        input.fake.pointer_motion_absolute(x, y);
        input.sync().map_err(|f| Failure { sent: false, ..f })?;
        thread::sleep(SETTLE);
        let state = state(Some((x, y)))?;
        let cursor = state.get("cursor").and_then(Value::as_array).cloned().unwrap_or_default();
        let at = |i: usize| cursor.get(i).and_then(Value::as_f64).unwrap_or(f64::NAN);
        if (at(0) - x).abs() <= 1.0 && (at(1) - y).abs() <= 1.0 {
            let top = state.get("at").and_then(Value::as_array).and_then(|a| a.first()).and_then(|w| w.get("id"));
            if top.and_then(Value::as_str) != Some(id) {
                return Err(failure(
                    "TARGET_UNAVAILABLE",
                    format!(
                        "The point ({x:.0}, {y:.0}) shows {}, not the target; the click would not reach it",
                        describe_at(&state)
                    ),
                ));
            }
            return Ok(());
        }
        if Instant::now() >= deadline {
            return Err(failure(
                "TARGET_UNAVAILABLE",
                format!(
                    "The pointer stayed at ({}, {}) instead of moving to ({x:.0}, {y:.0}); the mouse may be in use",
                    at(0),
                    at(1)
                ),
            ));
        }
    }
}

/// Clicks count times at a point of the target.
pub fn click(input: &mut FakeInput, id: &str, x: f64, y: f64, button: u32, count: u32) -> Result<(), Failure> {
    point_at(input, id, x, y)?;
    let possible = |f: Failure| Failure { sent: true, ..f };
    for i in 0..count {
        if i > 0 {
            thread::sleep(CLICK_GAP);
        }
        input.button(button, true).map_err(possible)?;
        input.button(button, false).map_err(possible)?;
    }
    Ok(())
}

/// Presses on `from` of the target and releases on `to`.
pub fn drag(input: &mut FakeInput, id: &str, from: (f64, f64), to: (f64, f64)) -> Result<(), Failure> {
    point_at(input, id, from.0, from.1)?;
    let left = 0x110;
    let result = (|| {
        input.button(left, true)?;
        for i in 1..=10 {
            thread::sleep(Duration::from_millis(10));
            let t = f64::from(i) / 10.0;
            input.fake.pointer_motion_absolute(from.0 + (to.0 - from.0) * t, from.1 + (to.1 - from.1) * t);
            input.sync()?;
        }
        Ok(())
    })();
    let released = input.button(left, false);
    result.and(released).map_err(|f| Failure { sent: true, ..f })
}

/// Refuses keys unless the target is KWin's active window.
fn require_active(id: &str) -> Result<(), Failure> {
    let state = state(None)?;
    if state.get("active").and_then(Value::as_str) != Some(id) {
        return Err(failure("TARGET_UNAVAILABLE", "The target window is not active; keys would reach another window"));
    }
    Ok(())
}

/// Presses modifiers and a key, releasing them in reverse.
pub fn key_chord(input: &mut FakeInput, id: &str, modifiers: &[u32], key: u32) -> Result<(), Failure> {
    let mut codes = Vec::new();
    for keysym in modifiers {
        codes.push(input.resolve(*keysym).ok_or_else(|| missing_key(*keysym))?.0);
    }
    let (code, shift) = input.resolve(key).ok_or_else(|| missing_key(key))?;
    if shift && !modifiers.contains(&SHIFT_L) {
        codes.push(input.resolve(SHIFT_L).ok_or_else(|| missing_key(SHIFT_L))?.0);
    }
    codes.push(code);
    require_active(id)?;
    let mut pressed = Vec::new();
    let result = codes.iter().try_for_each(|code| {
        input.key(*code, true)?;
        pressed.push(*code);
        Ok(())
    });
    let released = pressed.iter().rev().try_for_each(|code| input.key(*code, false));
    result.and(released).map_err(|f| Failure { sent: true, ..f })
}

fn missing_key(keysym: u32) -> Failure {
    failure("UNSUPPORTED_CAPABILITY", format!("The keyboard layout has no key for keysym {keysym:#x}"))
}

/// Types text through the layout, checking before each character that the
/// target is still active. Characters the layout lacks are refused before any is sent.
pub fn text(input: &mut FakeInput, id: &str, text: &str) -> Result<(), Failure> {
    let missing: Vec<char> = text.chars().filter(|c| input.resolve_char(*c).is_none()).collect();
    if !missing.is_empty() {
        return Err(failure(
            "UNSUPPORTED_CAPABILITY",
            format!("The keyboard layout cannot type {missing:?}; on KDE such text needs a focused text field"),
        ));
    }
    let shift = input.resolve(SHIFT_L).map(|(code, _)| code);
    for (i, character) in text.chars().enumerate() {
        let possible = |f: Failure| if i > 0 { Failure { sent: true, ..f } } else { f };
        require_active(id).map_err(possible)?;
        let (code, needs_shift) = input.resolve_char(character).unwrap();
        let mut codes = Vec::new();
        if needs_shift {
            codes.push(shift.ok_or_else(|| missing_key(SHIFT_L))?);
        }
        codes.push(code);
        let mut pressed = Vec::new();
        let result = codes.iter().try_for_each(|code| {
            input.key(*code, true)?;
            pressed.push(*code);
            Ok(())
        });
        let released = pressed.iter().rev().try_for_each(|code| input.key(*code, false));
        result.and(released).map_err(|f| Failure { sent: true, ..f })?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn capture_formats_map_to_frame_formats() {
        assert_eq!(frame_format(6), Some(1));
        assert_eq!(frame_format(4), Some(1));
        assert_eq!(frame_format(17), Some(0x3432_4258));
        assert_eq!(frame_format(3), None);
    }

    #[test]
    fn refusals_name_what_is_at_the_point() {
        let state = json!({"at": [{"id": "a", "caption": "", "dock": true, "notification": false}]});
        assert_eq!(describe_at(&state), "the panel \"\"");
        let state = json!({"at": [{"id": "b", "caption": "Firefox", "dock": false, "notification": false}]});
        assert_eq!(describe_at(&state), "the window \"Firefox\"");
        assert_eq!(describe_at(&json!({"at": []})), "nothing");
        let windows = json!({"windows": [{"id": "x", "visible": false}]});
        assert_eq!(visible_target(&windows, "x").unwrap_err().code, "TARGET_UNAVAILABLE");
        assert!(visible_target(&json!({"windows": [{"id": "x", "visible": true}]}), "x").is_ok());
    }
}
