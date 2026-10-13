//! wlroots virtual pointer: absolute motion over the output layout, and buttons.
//! The helper only moves and presses; the runtime moves first, confirms through
//! compositor IPC that the target window took focus under the pointer, and only
//! then asks for a button.

use std::time::Instant;

use wayland_client::{
    Connection, Dispatch, EventQueue, QueueHandle,
    globals::{GlobalListContents, registry_queue_init},
    protocol::{wl_output, wl_pointer, wl_registry, wl_seat},
};
use wayland_protocols::xdg::xdg_output::zv1::client::{zxdg_output_manager_v1::ZxdgOutputManagerV1, zxdg_output_v1};
use wayland_protocols_wlr::virtual_pointer::v1::client::{
    zwlr_virtual_pointer_manager_v1::ZwlrVirtualPointerManagerV1, zwlr_virtual_pointer_v1::ZwlrVirtualPointerV1,
};

use crate::keyboard::Failure;

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// Evdev codes for the buttons the runtime names.
pub fn button_code(name: &str) -> Option<u32> {
    match name {
        "left" => Some(0x110),
        "right" => Some(0x111),
        "middle" => Some(0x112),
        _ => None,
    }
}

/// Logical rectangle of the whole output layout, the space absolute motion maps to.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Layout {
    pub x: i32,
    pub y: i32,
    pub width: i32,
    pub height: i32,
}

impl Layout {
    /// The bounding box of the outputs' logical rectangles, as wlroots maps an
    /// absolute device that is not bound to one output.
    pub fn of(outputs: &[(i32, i32, i32, i32)]) -> Option<Layout> {
        let (mut x0, mut y0, mut x1, mut y1) = (i32::MAX, i32::MAX, i32::MIN, i32::MIN);
        for &(x, y, width, height) in outputs.iter().filter(|o| o.2 > 0 && o.3 > 0) {
            x0 = x0.min(x);
            y0 = y0.min(y);
            x1 = x1.max(x + width);
            y1 = y1.max(y + height);
        }
        (x0 < x1 && y0 < y1).then(|| Layout { x: x0, y: y0, width: x1 - x0, height: y1 - y0 })
    }

    /// Absolute motion arguments for a layout point, in hundredths of a pixel.
    pub fn motion(&self, x: f64, y: f64) -> Option<(u32, u32, u32, u32)> {
        let (dx, dy) = (x - f64::from(self.x), y - f64::from(self.y));
        let (width, height) = (f64::from(self.width), f64::from(self.height));
        if !(0.0..width).contains(&dx) || !(0.0..height).contains(&dy) {
            return None;
        }
        Some(((dx * 100.0) as u32, (dy * 100.0) as u32, self.width as u32 * 100, self.height as u32 * 100))
    }
}

#[derive(Default)]
struct State {
    outputs: Vec<(i32, i32, i32, i32)>,
}

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
impl Dispatch<wl_output::WlOutput, ()> for State {
    fn event(
        _: &mut Self,
        _: &wl_output::WlOutput,
        _: wl_output::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<ZxdgOutputManagerV1, ()> for State {
    fn event(
        _: &mut Self,
        _: &ZxdgOutputManagerV1,
        _: <ZxdgOutputManagerV1 as wayland_client::Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<zxdg_output_v1::ZxdgOutputV1, usize> for State {
    fn event(
        state: &mut Self,
        _: &zxdg_output_v1::ZxdgOutputV1,
        event: zxdg_output_v1::Event,
        index: &usize,
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        let output = &mut state.outputs[*index];
        match event {
            zxdg_output_v1::Event::LogicalPosition { x, y } => (output.0, output.1) = (x, y),
            zxdg_output_v1::Event::LogicalSize { width, height } => (output.2, output.3) = (width, height),
            _ => {}
        }
    }
}
impl Dispatch<ZwlrVirtualPointerManagerV1, ()> for State {
    fn event(
        _: &mut Self,
        _: &ZwlrVirtualPointerManagerV1,
        _: <ZwlrVirtualPointerManagerV1 as wayland_client::Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}
impl Dispatch<ZwlrVirtualPointerV1, ()> for State {
    fn event(
        _: &mut Self,
        _: &ZwlrVirtualPointerV1,
        _: <ZwlrVirtualPointerV1 as wayland_client::Proxy>::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}

/// Reads the current output layout over a connection of its own, so outputs
/// added or moved since the pointer was created are seen.
pub fn read_layout() -> Result<Layout, Failure> {
    let unavailable =
        |e: String| failure("DEPENDENCY_MISSING", format!("The compositor does not report output positions: {e}"));
    let connection = Connection::connect_to_env().map_err(|e| unavailable(e.to_string()))?;
    let (globals, mut queue) = registry_queue_init::<State>(&connection).map_err(|e| unavailable(e.to_string()))?;
    let handle = queue.handle();
    let outputs: ZxdgOutputManagerV1 = globals.bind(&handle, 2..=3, ()).map_err(|e| unavailable(e.to_string()))?;
    let mut state = State::default();
    for global in globals.contents().clone_list().into_iter().filter(|g| g.interface == "wl_output") {
        let output: wl_output::WlOutput = globals.registry().bind(global.name, global.version.min(4), &handle, ());
        outputs.get_xdg_output(&output, &handle, state.outputs.len());
        state.outputs.push((0, 0, 0, 0));
    }
    queue.roundtrip(&mut state).map_err(|e| failure("TARGET_UNAVAILABLE", format!("Wayland connection lost: {e}")))?;
    Layout::of(&state.outputs).ok_or_else(|| failure("TARGET_UNAVAILABLE", "The compositor has no output to point at"))
}

/// One virtual pointer on the default seat, kept for the helper's lifetime.
/// Dropping it releases held buttons and destroys the device.
pub struct VirtualPointer {
    _connection: Connection,
    queue: EventQueue<State>,
    pointer: ZwlrVirtualPointerV1,
    layout: Option<Layout>,
    held: Vec<u32>,
    start: Instant,
}

impl VirtualPointer {
    pub fn connect() -> Result<Self, Failure> {
        let unavailable =
            |e: String| failure("DEPENDENCY_MISSING", format!("The compositor offers no virtual pointer: {e}"));
        let connection = Connection::connect_to_env().map_err(|e| unavailable(e.to_string()))?;
        let (globals, queue) = registry_queue_init::<State>(&connection).map_err(|e| unavailable(e.to_string()))?;
        let handle = queue.handle();
        let seat: wl_seat::WlSeat = globals.bind(&handle, 1..=1, ()).map_err(|e| unavailable(e.to_string()))?;
        let manager: ZwlrVirtualPointerManagerV1 =
            globals.bind(&handle, 1..=2, ()).map_err(|e| unavailable(e.to_string()))?;
        let pointer = manager.create_virtual_pointer(Some(&seat), &handle, ());
        let mut device = VirtualPointer {
            _connection: connection,
            queue,
            pointer,
            layout: None,
            held: Vec::new(),
            start: Instant::now(),
        };
        device.sync()?;
        // A seat without any pointer gains the capability only now, and clients bind
        // their wl_pointer asynchronously: events sent before that are lost. The
        // device is kept, so this happens once per helper.
        std::thread::sleep(std::time::Duration::from_millis(200));
        Ok(device)
    }

    fn time(&self) -> u32 {
        self.start.elapsed().as_millis() as u32
    }

    /// Waits until the compositor has handled everything sent so far.
    fn sync(&mut self) -> Result<(), Failure> {
        self.queue.roundtrip(&mut State::default()).map(|_| ()).map_err(|e| Failure {
            code: "TARGET_UNAVAILABLE",
            message: format!("Wayland connection lost: {e}"),
            sent: true,
        })
    }

    /// Moves the pointer to a point in layout coordinates. The layout is read again
    /// for each gesture; motion while a button is held keeps the gesture's layout.
    pub fn move_to(&mut self, x: f64, y: f64) -> Result<(), Failure> {
        if self.held.is_empty() || self.layout.is_none() {
            self.layout = Some(read_layout()?);
        }
        let layout = self.layout.unwrap();
        let (x, y, width, height) = layout
            .motion(x, y)
            .ok_or_else(|| failure("INVALID_ARGUMENT", format!("({x}, {y}) is outside the outputs")))?;
        self.pointer.motion_absolute(self.time(), x, y, width, height);
        self.pointer.frame();
        self.sync()
    }

    pub fn press(&mut self, code: u32) -> Result<(), Failure> {
        self.pointer.button(self.time(), code, wl_pointer::ButtonState::Pressed);
        self.pointer.frame();
        self.held.push(code);
        self.sync()
    }

    pub fn release(&mut self, code: u32) -> Result<(), Failure> {
        self.pointer.button(self.time(), code, wl_pointer::ButtonState::Released);
        self.pointer.frame();
        self.held.retain(|held| *held != code);
        self.sync()
    }

    /// Clicks count times where the pointer is.
    pub fn click(&mut self, code: u32, count: u32) -> Result<(), Failure> {
        for _ in 0..count {
            self.press(code)?;
            self.release(code)?;
        }
        Ok(())
    }
}

impl Drop for VirtualPointer {
    fn drop(&mut self) {
        for code in std::mem::take(&mut self.held) {
            self.pointer.button(self.time(), code, wl_pointer::ButtonState::Released);
            self.pointer.frame();
        }
        self.pointer.destroy();
        let _ = self.queue.roundtrip(&mut State::default());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn layout_spans_every_output() {
        let layout = Layout::of(&[(0, 0, 1024, 768), (1024, -200, 1920, 1080), (0, 0, 0, 0)]).unwrap();
        assert_eq!(layout, Layout { x: 0, y: -200, width: 2944, height: 1080 });
        assert_eq!(layout.motion(1024.5, 0.0), Some((102450, 20000, 294400, 108000)));
        assert_eq!(layout.motion(2944.0, 0.0), None);
        assert_eq!(layout.motion(-1.0, 0.0), None);
        assert_eq!(Layout::of(&[]), None);
    }
}
