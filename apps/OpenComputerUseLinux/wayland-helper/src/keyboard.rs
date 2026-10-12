//! Keyboard input through libei. The session comes from the RemoteDesktop portal
//! (the user approves it in a system dialog) or, in isolated test sessions only,
//! from Mutter's own RemoteDesktop API. Keys go to whatever the compositor has
//! focused; the runtime checks the target's focus before every call.

use std::{
    collections::HashMap,
    fs::File,
    io::Read,
    os::{
        fd::{AsRawFd, OwnedFd},
        unix::net::UnixStream,
    },
    time::{Duration, Instant},
};

use reis::{PendingRequestResult, ei};

use crate::keymap::{Key, Keymap, SHIFT_L};

/// An error and whether input may already have reached the desktop.
pub struct Failure {
    pub code: &'static str,
    pub message: String,
    pub sent: bool,
}

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// Keeps whatever owns the remote-desktop session alive; dropping it ends the session.
pub enum Grant {
    Portal { runtime: tokio::runtime::Runtime, session: Box<dyn std::any::Any> },
    Mutter { _bus: zbus::blocking::Connection },
}

pub struct Keyboard {
    _grant: Grant,
    context: ei::Context,
    connection: ei::Connection,
    device: Option<ei::Device>,
    keyboard: Option<ei::Keyboard>,
    keymap: Option<Keymap>,
    resumed: bool,
    emulating: bool,
    serial: u32,
    sequence: u32,
    locked: u32,
    group: u32,
    held: Vec<u32>,
    last_time: u64,
    callbacks: HashMap<ei::callback::Callback, bool>,
}

impl Keyboard {
    /// Asks the RemoteDesktop portal for a keyboard; blocks while the user decides.
    pub fn grant_portal() -> Result<Self, Failure> {
        use ashpd::desktop::{
            CreateSessionOptions, PersistMode,
            remote_desktop::{ConnectToEISOptions, DeviceType, RemoteDesktop, SelectDevicesOptions, StartOptions},
        };
        let runtime = tokio::runtime::Builder::new_current_thread().enable_all().build().map_err(|e| failure("DEPENDENCY_MISSING", e.to_string()))?;
        let (session, fd) = runtime
            .block_on(async {
                let portal = RemoteDesktop::new().await?;
                let session = portal.create_session(CreateSessionOptions::default()).await?;
                portal
                    .select_devices(&session, SelectDevicesOptions::default().set_devices(ashpd::enumflags2::BitFlags::from(DeviceType::Keyboard)).set_persist_mode(PersistMode::DoNot))
                    .await?;
                portal.start(&session, None, StartOptions::default()).await?.response()?;
                let fd = portal.connect_to_eis(&session, ConnectToEISOptions::default()).await?;
                Ok::<_, ashpd::Error>((session, fd))
            })
            .map_err(|error| match error {
                ashpd::Error::Response(_) => failure("PERMISSION_REQUIRED", "Remote input was not allowed in the system dialog"),
                other => failure("DEPENDENCY_MISSING", format!("The RemoteDesktop portal is unavailable: {other}")),
            })?;
        Self::connect(Grant::Portal { runtime, session: Box::new(session) }, fd)
    }

    /// Uses Mutter's RemoteDesktop API without a dialog. Only for isolated test
    /// sessions; the caller must have checked the test opt-in.
    pub fn grant_mutter_for_test() -> Result<Self, Failure> {
        let unavailable = |e: zbus::Error| failure("DEPENDENCY_MISSING", format!("Mutter RemoteDesktop is unavailable: {e}"));
        let bus = zbus::blocking::Connection::session().map_err(unavailable)?;
        let service = zbus::blocking::Proxy::new(&bus, "org.gnome.Mutter.RemoteDesktop", "/org/gnome/Mutter/RemoteDesktop", "org.gnome.Mutter.RemoteDesktop").map_err(unavailable)?;
        let path: zbus::zvariant::OwnedObjectPath = service.call("CreateSession", &()).map_err(unavailable)?;
        let session = zbus::blocking::Proxy::new(&bus, "org.gnome.Mutter.RemoteDesktop", path, "org.gnome.Mutter.RemoteDesktop.Session").map_err(unavailable)?;
        session.call::<_, _, ()>("Start", &()).map_err(unavailable)?;
        let options: HashMap<&str, zbus::zvariant::Value> = HashMap::new();
        let fd: zbus::zvariant::OwnedFd = session.call("ConnectToEIS", &(options,)).map_err(unavailable)?;
        Self::connect(Grant::Mutter { _bus: bus }, fd.into())
    }

    fn connect(grant: Grant, fd: OwnedFd) -> Result<Self, Failure> {
        let stream = UnixStream::from(fd);
        let context = ei::Context::new(stream).map_err(|e| failure("TARGET_UNAVAILABLE", e.to_string()))?;
        let handshake = reis::handshake::ei_handshake_blocking(&context, "open-computer-use", ei::handshake::ContextType::Sender)
            .map_err(|e| failure("TARGET_UNAVAILABLE", format!("EIS handshake failed: {e}")))?;
        let mut keyboard = Keyboard {
            _grant: grant,
            context,
            connection: handshake.connection,
            device: None,
            keyboard: None,
            keymap: None,
            resumed: false,
            emulating: false,
            serial: handshake.serial,
            sequence: 0,
            locked: 0,
            group: 0,
            held: Vec::new(),
            last_time: 0,
            callbacks: HashMap::new(),
        };
        let deadline = Instant::now() + Duration::from_secs(5);
        while !(keyboard.resumed && keyboard.keyboard.is_some() && keyboard.keymap.is_some()) {
            if Instant::now() > deadline {
                return Err(failure("TARGET_UNAVAILABLE", "The compositor did not provide a keyboard device"));
            }
            keyboard.pump(Duration::from_millis(100))?;
        }
        Ok(keyboard)
    }

    /// Reads and handles whatever events arrive within the timeout.
    fn pump(&mut self, timeout: Duration) -> Result<(), Failure> {
        let mut poll = libc::pollfd { fd: self.context.as_raw_fd(), events: libc::POLLIN, revents: 0 };
        let ready = unsafe { libc::poll(&mut poll, 1, timeout.as_millis() as i32) };
        if ready > 0 && self.context.read().map_err(|e| failure("TARGET_UNAVAILABLE", format!("EIS connection lost: {e}")))? == 0 {
            return Err(failure("TARGET_UNAVAILABLE", "The compositor closed the EIS connection"));
        }
        while let Some(result) = self.context.pending_event() {
            let PendingRequestResult::Request(event) = result else { continue };
            self.handle(event)?;
        }
        let _ = self.context.flush();
        Ok(())
    }

    fn handle(&mut self, event: ei::Event) -> Result<(), Failure> {
        match event {
            ei::Event::Connection(_, ei::connection::Event::Ping { ping }) => ping.done(0),
            ei::Event::Connection(_, ei::connection::Event::Disconnected { explanation, .. }) => {
                return Err(failure("TARGET_UNAVAILABLE", format!("The compositor ended remote input: {}", explanation.unwrap_or_default())));
            }
            ei::Event::Seat(seat, ei::seat::Event::Capability { mask, interface }) if interface == "ei_keyboard" => {
                self.callbacks.clear();
                seat.bind(mask);
            }
            ei::Event::Seat(_, ei::seat::Event::Device { device }) => {
                if self.device.is_none() {
                    self.device = Some(device);
                }
            }
            ei::Event::Device(device, event) if Some(&device) == self.device.as_ref() => match event {
                ei::device::Event::Interface { object } => {
                    if let Some(keyboard) = object.downcast::<ei::Keyboard>() {
                        self.keyboard = Some(keyboard);
                    }
                }
                ei::device::Event::Resumed { serial } => {
                    self.serial = serial;
                    self.resumed = true;
                }
                ei::device::Event::Paused { serial } => {
                    self.serial = serial;
                    self.resumed = false;
                    self.emulating = false;
                }
                _ => {}
            },
            ei::Event::Keyboard(_, ei::keyboard::Event::Keymap { size, keymap, .. }) => {
                let mut text = Vec::with_capacity(size as usize);
                File::from(keymap).take(size as u64).read_to_end(&mut text).map_err(|e| failure("TARGET_UNAVAILABLE", e.to_string()))?;
                self.keymap = Some(Keymap::parse(&text).map_err(|e| failure("DEPENDENCY_MISSING", e))?);
            }
            ei::Event::Keyboard(_, ei::keyboard::Event::Modifiers { serial, locked, group, .. }) => {
                self.serial = serial;
                self.locked = locked;
                self.group = group;
            }
            ei::Event::Callback(callback, ei::callback::Event::Done { .. }) => {
                self.callbacks.insert(callback, true);
            }
            _ => {}
        }
        Ok(())
    }

    /// Waits until the compositor has processed everything sent so far.
    fn sync(&mut self) -> Result<(), Failure> {
        let callback = self.connection.sync(1);
        self.callbacks.insert(callback.clone(), false);
        let deadline = Instant::now() + Duration::from_secs(2);
        while self.callbacks.get(&callback) == Some(&false) {
            if Instant::now() > deadline {
                return Err(failure("TARGET_UNAVAILABLE", "The compositor did not acknowledge input"));
            }
            self.pump(Duration::from_millis(50))?;
        }
        self.callbacks.remove(&callback);
        Ok(())
    }

    fn timestamp(&mut self) -> u64 {
        let mut now = libc::timespec { tv_sec: 0, tv_nsec: 0 };
        unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut now) };
        // EIS frames need strictly increasing monotonic microseconds.
        let micros = (now.tv_sec as u64) * 1_000_000 + (now.tv_nsec as u64) / 1_000;
        self.last_time = micros.max(self.last_time + 1);
        self.last_time
    }

    fn ready(&mut self) -> Result<(), Failure> {
        self.pump(Duration::ZERO)?;
        if !self.resumed {
            return Err(failure("TARGET_UNAVAILABLE", "The compositor paused remote input"));
        }
        if !self.emulating {
            self.sequence += 1;
            self.device.as_ref().unwrap().start_emulating(self.serial, self.sequence);
            self.emulating = true;
        }
        Ok(())
    }

    fn key(&mut self, code: u32, press: bool) {
        // EIS uses evdev codes, XKB keycodes minus 8.
        let evdev = code - 8;
        let state = if press { ei::keyboard::KeyState::Press } else { ei::keyboard::KeyState::Released };
        self.keyboard.as_ref().unwrap().key(evdev, state);
        let time = self.timestamp();
        self.device.as_ref().unwrap().frame(self.serial, time);
        if press {
            self.held.push(code);
        } else {
            self.held.retain(|held| *held != code);
        }
    }

    fn resolve(&self, keysym: u32) -> Option<Key> {
        self.keymap.as_ref()?.keysym(keysym, self.group)
    }

    /// Characters the current layout cannot produce at its first two levels.
    pub fn missing(&mut self, text: &str) -> Result<Vec<char>, Failure> {
        self.pump(Duration::ZERO)?;
        let keymap = self.keymap.as_ref().unwrap();
        let mut missing: Vec<char> = text.chars().filter(|c| keymap.character(*c, self.group).is_none()).collect();
        missing.dedup();
        Ok(missing)
    }

    /// Presses the modifiers, then the key, then releases them in reverse.
    pub fn chord(&mut self, modifiers: &[u32], keysym: u32) -> Result<(), Failure> {
        self.pump(Duration::ZERO)?;
        let mut codes = Vec::new();
        for modifier in modifiers {
            codes.push(self.resolve(*modifier).ok_or_else(|| failure("UNSUPPORTED_CAPABILITY", format!("The keyboard layout has no modifier {modifier:#x}")))?.code);
        }
        let key = self.resolve(keysym).ok_or_else(|| failure("UNSUPPORTED_CAPABILITY", format!("The keyboard layout has no key {keysym:#x}")))?;
        if key.shift && !modifiers.contains(&SHIFT_L) {
            codes.push(self.resolve(SHIFT_L).ok_or_else(|| failure("UNSUPPORTED_CAPABILITY", "The keyboard layout has no Shift key"))?.code);
        }
        self.send(&codes, key.code)
    }

    /// Types text the layout can produce; refuses before sending when it cannot.
    pub fn text(&mut self, text: &str) -> Result<(), Failure> {
        let missing = self.missing(text)?;
        if !missing.is_empty() {
            return Err(failure("UNSUPPORTED_CAPABILITY", format!("The keyboard layout cannot type {:?}", missing.iter().collect::<String>())));
        }
        let keymap = self.keymap.as_ref().unwrap();
        if keymap.caps_locked(self.locked) && text.chars().any(char::is_alphabetic) {
            return Err(failure("TARGET_UNAVAILABLE", "Caps Lock is on; letters would change case"));
        }
        let shift = self.resolve(SHIFT_L).map(|k| k.code);
        let keys: Vec<Key> = text.chars().map(|c| keymap.character(c, self.group).unwrap()).collect();
        let mut sent = false;
        for key in keys {
            let modifiers: Vec<u32> = if key.shift { shift.into_iter().collect() } else { Vec::new() };
            self.send(&modifiers, key.code).map_err(|mut f| {
                f.sent |= sent;
                f
            })?;
            sent = true;
            // The compositor acknowledging an event does not mean the client has read it.
            std::thread::sleep(Duration::from_millis(5));
        }
        Ok(())
    }

    fn send(&mut self, modifiers: &[u32], code: u32) -> Result<(), Failure> {
        self.ready()?;
        for modifier in modifiers {
            self.key(*modifier, true);
        }
        self.key(code, true);
        self.key(code, false);
        for modifier in modifiers.iter().rev() {
            self.key(*modifier, false);
        }
        self.sync().map_err(|mut f| {
            f.sent = true;
            f
        })
    }

    /// Presses and keeps a key down; only the isolated cleanup test uses this.
    pub fn hold_for_test(&mut self, keysym: u32) -> Result<(), Failure> {
        let key = self.resolve(keysym).ok_or_else(|| failure("UNSUPPORTED_CAPABILITY", "no such key"))?;
        self.ready()?;
        self.key(key.code, true);
        self.sync()
    }

    /// Releases held keys and stops emulating, then waits for the compositor.
    pub fn release_all(&mut self) {
        if self.emulating {
            for code in self.held.clone().into_iter().rev() {
                self.key(code, false);
            }
            self.device.as_ref().unwrap().stop_emulating(self.serial);
            self.emulating = false;
            let _ = self.context.flush();
            let _ = self.sync();
        }
    }
}

impl Drop for Keyboard {
    fn drop(&mut self) {
        self.release_all();
        if let Grant::Portal { runtime, session } = &mut self._grant {
            if let Some(session) = session.downcast_mut::<ashpd::desktop::Session<ashpd::desktop::remote_desktop::RemoteDesktop>>() {
                let _ = runtime.block_on(session.close());
            }
        }
    }
}
