//! wlroots-family input (Sway, Hyprland, labwc, ...): the virtual keyboard
//! protocol, plus compositor IPC that names the focused window so input can be
//! bound to its target. No dialog is involved; the runtime gates use itself.

use std::{
    io::{Read, Write},
    os::{
        fd::{AsFd, FromRawFd, OwnedFd},
        unix::net::UnixStream,
    },
    time::Duration,
};

use serde_json::Value;
use wayland_client::{
    Connection, Dispatch, EventQueue, QueueHandle,
    globals::{GlobalListContents, registry_queue_init},
    protocol::{wl_registry, wl_seat},
};
use wayland_protocols_misc::zwp_virtual_keyboard_v1::client::{
    zwp_virtual_keyboard_manager_v1::ZwpVirtualKeyboardManagerV1, zwp_virtual_keyboard_v1::ZwpVirtualKeyboardV1,
};

use crate::keyboard::Failure;

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// Keysym for a character in the X11 convention; control characters other than
/// newline and tab have none.
pub fn char_keysym(c: char) -> Option<u32> {
    match c {
        '\n' | '\r' => Some(0xff0d),
        '\t' => Some(0xff09),
        c if c.is_control() => None,
        c if (' '..='~').contains(&c) || ('\u{a0}'..='\u{ff}').contains(&c) => Some(c as u32),
        c => Some(0x0100_0000 + c as u32),
    }
}

/// An XKB keymap with one keycode per keysym, starting at keycode 9. Keysyms are
/// written as numbers, so any character works without a keysym name table.
pub fn keymap(keysyms: &[u32]) -> String {
    let mut text = String::from("xkb_keymap {\nxkb_keycodes \"ocu\" {\nminimum = 8;\n");
    text += &format!("maximum = {};\n", 9 + keysyms.len().max(1));
    for i in 0..keysyms.len() {
        text += &format!("<K{i}> = {};\n", 9 + i);
    }
    text += "};\nxkb_types \"ocu\" { include \"complete\" };\nxkb_compatibility \"ocu\" { include \"complete\" };\nxkb_symbols \"ocu\" {\n";
    for (i, keysym) in keysyms.iter().enumerate() {
        text += &format!("key <K{i}> {{ [ {keysym:#x} ] }};\n");
    }
    // Modifier keys change the modifier state the way a physical keyboard does.
    for (i, keysym) in keysyms.iter().enumerate() {
        if let Some(name) = modifier_name(*keysym) {
            text += &format!("modifier_map {name} {{ <K{i}> }};\n");
        }
    }
    text += "};\n};\n";
    text
}

fn modifier_bit(keysym: u32) -> u32 {
    match keysym {
        0xffe1 => 1,
        0xffe3 => 4,
        0xffe9 => 8,
        0xffeb => 64,
        _ => 0,
    }
}

/// Shift_L, Control_L, Alt_L and Super_L, at keycodes 9 to 12 on every device.
const MODIFIERS: [u32; 4] = [0xffe1, 0xffe3, 0xffe9, 0xffeb];
const MAX_KEYS: usize = 247;

/// Keys every device maps at the same keycodes: modifiers, printable ASCII, the
/// named keys press_key accepts and F1 to F24. Clients may resolve shortcuts
/// against an earlier keymap, even another device's, so these never move; only
/// other characters are appended after them.
fn base_keysyms() -> Vec<u32> {
    let mut keysyms = MODIFIERS.to_vec();
    keysyms.extend(0x20..=0x7e);
    keysyms.extend([
        0xff0d, 0xff09, 0xff1b, 0xff08, 0xffff, 0xff63, 0xff50, 0xff51, 0xff52, 0xff53, 0xff54, 0xff55, 0xff56, 0xff57,
        0xff67,
    ]);
    keysyms.extend(0xffbe..=0xffd5);
    keysyms
}

/// Real modifier names for the modifier keysyms press_key accepts.
fn modifier_name(keysym: u32) -> Option<&'static str> {
    match keysym {
        0xffe1 => Some("Shift"),
        0xffe3 => Some("Control"),
        0xffe9 => Some("Mod1"),
        0xffeb => Some("Mod4"),
        _ => None,
    }
}

struct State;

impl Dispatch<wl_registry::WlRegistry, GlobalListContents> for State {
    fn event(
        _: &mut Self,
        _: &wl_registry::WlRegistry,
        _: wl_registry::Event,
        _: &GlobalListContents,
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<wl_seat::WlSeat, ()> for State {
    fn event(_: &mut Self, _: &wl_seat::WlSeat, _: wl_seat::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<ZwpVirtualKeyboardManagerV1, ()> for State {
    fn event(
        _: &mut Self,
        _: &ZwpVirtualKeyboardManagerV1,
        _: <ZwpVirtualKeyboardManagerV1 as wayland_client::Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<ZwpVirtualKeyboardV1, ()> for State {
    fn event(
        _: &mut Self,
        _: &ZwpVirtualKeyboardV1,
        _: <ZwpVirtualKeyboardV1 as wayland_client::Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}

/// One virtual keyboard on the default seat. Dropping it releases held keys and
/// destroys the device.
pub struct VirtualKeyboard {
    _connection: Connection,
    queue: EventQueue<State>,
    keyboard: ZwpVirtualKeyboardV1,
    keysyms: Vec<u32>,
    held: Vec<u32>,
    time: u32,
}

impl VirtualKeyboard {
    pub fn connect() -> Result<Self, Failure> {
        let unavailable =
            |e: String| failure("DEPENDENCY_MISSING", format!("The compositor offers no virtual keyboard: {e}"));
        let connection = Connection::connect_to_env().map_err(|e| unavailable(e.to_string()))?;
        let (globals, queue) = registry_queue_init::<State>(&connection).map_err(|e| unavailable(e.to_string()))?;
        let handle = queue.handle();
        let seat: wl_seat::WlSeat = globals.bind(&handle, 1..=1, ()).map_err(|e| unavailable(e.to_string()))?;
        let manager: ZwpVirtualKeyboardManagerV1 =
            globals.bind(&handle, 1..=1, ()).map_err(|e| unavailable(e.to_string()))?;
        let keyboard = manager.create_virtual_keyboard(&seat, &handle, ());
        let mut device = VirtualKeyboard {
            _connection: connection,
            queue,
            keyboard,
            keysyms: Vec::new(),
            held: Vec::new(),
            time: 0,
        };
        // The protocol requires a keymap before any key; modifiers take the first,
        // fixed keycodes. Clearing the modifier state also undoes modifiers a crashed
        // client left behind: Hyprland keeps them on the seat until a keyboard reports new ones.
        device.upload(&base_keysyms())?;
        device.keyboard.modifiers(0, 0, 0, 0);
        device.sync()?;
        Ok(device)
    }

    fn sync(&mut self) -> Result<(), Failure> {
        self.queue
            .roundtrip(&mut State)
            .map(|_| ())
            .map_err(|e| failure("TARGET_UNAVAILABLE", format!("Wayland connection lost: {e}")))
    }

    /// Makes sure every keysym has a keycode and returns their keymap indexes. Base
    /// keys never move; appended characters keep their keycodes until the keymap is full.
    fn ensure(&mut self, keysyms: &[u32]) -> Result<Vec<usize>, Failure> {
        let missing: Vec<u32> = keysyms.iter().copied().filter(|k| !self.keysyms.contains(k)).collect();
        if !missing.is_empty() {
            let mut next = self.keysyms.clone();
            for keysym in missing {
                if !next.contains(&keysym) {
                    next.push(keysym);
                }
            }
            if next.len() > MAX_KEYS {
                next = base_keysyms();
                for keysym in keysyms {
                    if !next.contains(keysym) {
                        next.push(*keysym);
                    }
                }
                if next.len() > MAX_KEYS {
                    return Err(failure("INVALID_ARGUMENT", "Too many distinct characters in one request"));
                }
            }
            self.upload(&next)?;
        }
        Ok(keysyms.iter().map(|k| self.keysyms.iter().position(|s| s == k).unwrap()).collect())
    }

    fn upload(&mut self, keysyms: &[u32]) -> Result<(), Failure> {
        let mut text = keymap(keysyms).into_bytes();
        text.push(0);
        let fd = unsafe { libc::memfd_create(c"ocu-keymap".as_ptr(), libc::MFD_CLOEXEC) };
        if fd < 0 {
            return Err(failure("TARGET_UNAVAILABLE", "Cannot create a keymap file"));
        }
        let mut file = std::fs::File::from(unsafe { OwnedFd::from_raw_fd(fd) });
        file.write_all(&text).map_err(|e| failure("TARGET_UNAVAILABLE", e.to_string()))?;
        self.keyboard.keymap(1, file.as_fd(), text.len() as u32);
        self.keysyms = keysyms.to_vec();
        self.sync()
    }

    fn timestamp(&mut self) -> u32 {
        self.time += 1;
        self.time
    }

    fn key(&mut self, index: usize, press: bool) {
        // Keycode 9 + index in the keymap is evdev code 1 + index.
        let code = 1 + index as u32;
        let time = self.timestamp();
        self.keyboard.key(time, code, if press { 1 } else { 0 });
        if press {
            self.held.push(code);
        } else {
            self.held.retain(|held| *held != code);
        }
    }

    /// Presses the modifier keys, reports the modifier state, then presses and
    /// releases key and undoes both in reverse.
    pub fn chord(&mut self, modifiers: &[u32], key: u32) -> Result<(), Failure> {
        for modifier in modifiers {
            modifier_name(*modifier)
                .ok_or_else(|| failure("INVALID_ARGUMENT", format!("Unsupported modifier {modifier:#x}")))?;
        }
        let mut keysyms = modifiers.to_vec();
        keysyms.push(key);
        let indexes = self.ensure(&keysyms)?;
        let (main, modifier_keys) = indexes.split_last().unwrap();
        let mask = modifiers.iter().map(|m| modifier_bit(*m)).fold(0, |a, b| a | b);
        for index in modifier_keys {
            self.key(*index, true);
        }
        // wlroots does not derive modifiers from a virtual keyboard's keys; the
        // client reports them, after its keys made it the seat's keyboard.
        self.keyboard.modifiers(mask, 0, 0, 0);
        self.key(*main, true);
        self.key(*main, false);
        self.keyboard.modifiers(0, 0, 0, 0);
        for index in modifier_keys.iter().rev() {
            self.key(*index, false);
        }
        self.sync().map_err(|mut f| {
            f.sent = true;
            f
        })
    }

    /// Types text; any Unicode character works because the keymap carries it.
    pub fn text(&mut self, text: &str) -> Result<(), Failure> {
        let keysyms: Vec<u32> = text
            .chars()
            .map(|c| {
                char_keysym(c)
                    .ok_or_else(|| failure("INVALID_ARGUMENT", format!("Cannot type control character {c:?}")))
            })
            .collect::<Result<_, _>>()?;
        self.keyboard.modifiers(0, 0, 0, 0);
        let mut sent = false;
        for chunk in keysyms.chunks(64) {
            let indexes = self.ensure(chunk).map_err(|mut f| {
                f.sent |= sent;
                f
            })?;
            for index in indexes {
                self.key(index, true);
                self.key(index, false);
                sent = true;
            }
            self.sync().map_err(|mut f| {
                f.sent = true;
                f
            })?;
            // The compositor acknowledging events does not mean the client has read them.
            std::thread::sleep(Duration::from_millis(5));
        }
        Ok(())
    }

    /// Presses and keeps Control and a key down; only the isolated cleanup test uses this.
    pub fn hold_for_test(&mut self, key: u32) -> Result<(), Failure> {
        let indexes = self.ensure(&[0xffe3, key])?;
        self.key(indexes[0], true);
        self.keyboard.modifiers(4, 0, 0, 0);
        self.key(indexes[1], true);
        self.sync()
    }
}

impl Drop for VirtualKeyboard {
    fn drop(&mut self) {
        for code in self.held.clone() {
            let time = self.timestamp();
            self.keyboard.key(time, code, 0);
        }
        self.keyboard.modifiers(0, 0, 0, 0);
        self.keyboard.destroy();
        let _ = self.sync();
    }
}

/// The compositor's focused window, from its IPC.
pub struct ActiveWindow {
    pub pid: Option<u32>,
    pub title: String,
    pub rect: Option<[i64; 4]>,
}

/// Reads the focused window from Sway or Hyprland IPC; `Ok(None)` when this
/// compositor has no IPC the helper knows.
pub fn active_window() -> Result<Option<ActiveWindow>, Failure> {
    let unavailable = |e: String| failure("TARGET_UNAVAILABLE", format!("Compositor IPC failed: {e}"));
    if let Ok(path) = std::env::var("SWAYSOCK") {
        let tree = sway_request(&path, 4).map_err(unavailable)?;
        return Ok(Some(focused_sway_node(&tree).unwrap_or(ActiveWindow {
            pid: None,
            title: String::new(),
            rect: None,
        })));
    }
    if std::env::var("HYPRLAND_INSTANCE_SIGNATURE").is_ok() {
        let window = hyprland_request("j/activewindow").map_err(unavailable)?;
        let pair = |name: &str| {
            window
                .get(name)
                .and_then(Value::as_array)
                .filter(|v| v.len() == 2)
                .map(|v| (v[0].as_i64().unwrap_or(0), v[1].as_i64().unwrap_or(0)))
        };
        return Ok(Some(ActiveWindow {
            pid: window.get("pid").and_then(Value::as_u64).and_then(|p| u32::try_from(p).ok()),
            title: window.get("title").and_then(Value::as_str).unwrap_or_default().to_owned(),
            rect: pair("at").zip(pair("size")).map(|((x, y), (w, h))| [x, y, w, h]),
        }));
    }
    Ok(None)
}

/// One Hyprland IPC request on the instance's control socket.
pub fn hyprland_request(command: &str) -> Result<Value, String> {
    let signature = std::env::var("HYPRLAND_INSTANCE_SIGNATURE").map_err(|e| e.to_string())?;
    let runtime = std::env::var("XDG_RUNTIME_DIR").map_err(|e| e.to_string())?;
    let mut stream =
        UnixStream::connect(format!("{runtime}/hypr/{signature}/.socket.sock")).map_err(|e| e.to_string())?;
    stream.set_read_timeout(Some(Duration::from_secs(2))).ok();
    stream.write_all(command.as_bytes()).map_err(|e| e.to_string())?;
    let mut reply = String::new();
    stream.read_to_string(&mut reply).map_err(|e| e.to_string())?;
    serde_json::from_str(&reply).map_err(|e| e.to_string())
}

/// One i3/Sway IPC request: "i3-ipc", payload length, message type, payload.
pub fn sway_request(path: &str, kind: u32) -> Result<Value, String> {
    let mut stream = UnixStream::connect(path).map_err(|e| e.to_string())?;
    stream.set_read_timeout(Some(Duration::from_secs(2))).ok();
    let mut request = b"i3-ipc".to_vec();
    request.extend_from_slice(&0u32.to_ne_bytes());
    request.extend_from_slice(&kind.to_ne_bytes());
    stream.write_all(&request).map_err(|e| e.to_string())?;
    let mut header = [0u8; 14];
    stream.read_exact(&mut header).map_err(|e| e.to_string())?;
    let length = u32::from_ne_bytes(header[6..10].try_into().unwrap()) as usize;
    let mut body = vec![0; length];
    stream.read_exact(&mut body).map_err(|e| e.to_string())?;
    serde_json::from_slice(&body).map_err(|e| e.to_string())
}

fn focused_sway_node(node: &Value) -> Option<ActiveWindow> {
    if node.get("focused").and_then(Value::as_bool) == Some(true) && node.get("pid").is_some() {
        let rect = node
            .get("rect")
            .map(|r| ["x", "y", "width", "height"].map(|k| r.get(k).and_then(Value::as_i64).unwrap_or(0)));
        return Some(ActiveWindow {
            pid: node.get("pid").and_then(Value::as_u64).and_then(|p| u32::try_from(p).ok()),
            title: node.get("name").and_then(Value::as_str).unwrap_or_default().to_owned(),
            rect,
        });
    }
    ["nodes", "floating_nodes"]
        .iter()
        .filter_map(|key| node.get(*key).and_then(Value::as_array))
        .flatten()
        .find_map(focused_sway_node)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::keymap::Keymap;

    #[test]
    fn keymap_carries_any_character() {
        let keysyms: Vec<u32> = "aZ中é\n".chars().map(|c| char_keysym(c).unwrap()).collect();
        assert_eq!(keysyms, vec![0x61, 0x5a, 0x0100_4e2d, 0xe9, 0xff0d]);
        assert_eq!(char_keysym('\u{7}'), None);
        let Ok(compiled) = Keymap::parse(keymap(&keysyms).as_bytes()) else {
            eprintln!("libxkbcommon is not installed; skipping compile check");
            return;
        };
        for (i, c) in "aZ中é".chars().enumerate() {
            assert_eq!(compiled.character(c, 0).map(|k| k.code), Some(9 + i as u32), "{c}");
        }
    }

    #[test]
    fn base_keys_fit_and_stay_fixed() {
        let base = base_keysyms();
        assert!(base.len() + 64 <= MAX_KEYS, "a 64-character text chunk must fit after the base keys");
        assert_eq!(&base[..4], &MODIFIERS);
        assert_eq!(base.iter().position(|k| *k == 'a' as u32), base_keysyms().iter().position(|k| *k == 'a' as u32));
        let mut sorted = base.clone();
        sorted.sort_unstable();
        sorted.dedup();
        assert_eq!(sorted.len(), base.len(), "no duplicate keysyms");
    }

    #[test]
    fn finds_the_focused_sway_window() {
        let tree = serde_json::json!({"nodes": [{"nodes": [
            {"focused": false, "pid": 1, "name": "other"},
            {"focused": true, "pid": 42, "name": "Target", "rect": {"x": 10, "y": 20, "width": 300, "height": 200}}]}]});
        let window = focused_sway_node(&tree).unwrap();
        assert_eq!((window.pid, window.title.as_str(), window.rect), (Some(42), "Target", Some([10, 20, 300, 200])));
    }
}
