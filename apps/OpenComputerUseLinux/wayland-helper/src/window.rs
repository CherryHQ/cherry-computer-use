//! Window location and capture on wlroots-family compositors. Sway and Hyprland
//! IPC name each window's process, title and layout geometry; Hyprland exports a
//! single window's pixels, while Sway can only copy an output region, which is
//! refused when another window could cover the target.

use std::{
    io::Read,
    os::fd::{AsFd, FromRawFd, OwnedFd},
    time::{Duration, Instant},
};

use base64::Engine as _;
use serde_json::{Value, json};
use wayland_client::{
    Connection, Dispatch, EventQueue, Proxy, QueueHandle, WEnum,
    globals::{GlobalListContents, registry_queue_init},
    protocol::{wl_buffer, wl_output, wl_registry, wl_shm, wl_shm_pool},
};
use wayland_protocols_wlr::{
    foreign_toplevel::v1::client::{zwlr_foreign_toplevel_handle_v1 as toplevel, zwlr_foreign_toplevel_manager_v1 as toplevel_manager},
    screencopy::v1::client::{zwlr_screencopy_frame_v1, zwlr_screencopy_manager_v1},
};

use crate::{keyboard::Failure, wlr};

#[allow(non_upper_case_globals, non_camel_case_types, unused_imports, missing_docs, clippy::all)]
pub mod hyprland_export {
    use wayland_client;
    use wayland_client::protocol::*;
    use wayland_protocols_wlr::foreign_toplevel::v1::client::*;
    pub mod __interfaces {
        use wayland_client::protocol::__interfaces::*;
        use wayland_protocols_wlr::foreign_toplevel::v1::client::__interfaces::*;
        wayland_scanner::generate_interfaces!("protocols/hyprland-toplevel-export-v1.xml");
    }
    use self::__interfaces::*;
    wayland_scanner::generate_client_code!("protocols/hyprland-toplevel-export-v1.xml");
}
use hyprland_export::{hyprland_toplevel_export_frame_v1 as export_frame, hyprland_toplevel_export_manager_v1 as export_manager};

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// A window as the compositor's IPC describes it, in layout (logical) pixels.
#[derive(Clone, Debug, PartialEq)]
pub struct Window {
    pub pid: u32,
    pub title: String,
    pub rect: [i64; 4],
    pub focused: bool,
    pub visible: bool,
    /// Hyprland window address, or the Sway output holding the window.
    pub address: Option<u64>,
    pub output: Option<String>,
    pub output_rect: Option<[i64; 4]>,
    /// Sway only: other visible floating windows on the same workspace.
    pub overlapping: Vec<[i64; 4]>,
}

fn rect(value: Option<&Value>) -> [i64; 4] {
    let get = |k: &str| value.and_then(|v| v.get(k)).and_then(Value::as_i64).unwrap_or(0);
    [get("x"), get("y"), get("width"), get("height")]
}

fn intersects(a: [i64; 4], b: [i64; 4]) -> bool {
    a[0] < b[0] + b[2] && b[0] < a[0] + a[2] && a[1] < b[1] + b[3] && b[1] < a[1] + a[3]
}

/// Collects Sway views with their output and workspace.
fn sway_views(node: &Value, output: Option<(&str, [i64; 4])>, workspace: Option<&str>, floating: bool, views: &mut Vec<(Window, String, bool)>) {
    let kind = node.get("type").and_then(Value::as_str).unwrap_or_default();
    let name = node.get("name").and_then(Value::as_str);
    let output = if kind == "output" { name.map(|n| (n, rect(node.get("rect")))) } else { output };
    let workspace = if kind == "workspace" { name } else { workspace };
    if let Some(pid) = node.get("pid").and_then(Value::as_u64) {
        views.push((
            Window {
                pid: pid as u32,
                title: name.unwrap_or_default().to_owned(),
                rect: rect(node.get("rect")),
                focused: node.get("focused").and_then(Value::as_bool) == Some(true),
                visible: node.get("visible").and_then(Value::as_bool) == Some(true),
                address: None,
                output: output.map(|(n, _)| n.to_owned()),
                output_rect: output.map(|(_, r)| r),
                overlapping: Vec::new(),
            },
            workspace.unwrap_or_default().to_owned(),
            floating,
        ));
    }
    for (key, floating) in [("nodes", floating), ("floating_nodes", true)] {
        for child in node.get(key).and_then(Value::as_array).into_iter().flatten() {
            sway_views(child, output, workspace, floating, views);
        }
    }
}

fn sway_windows(tree: &Value) -> Vec<Window> {
    let mut views = Vec::new();
    sway_views(tree, None, None, false, &mut views);
    views
        .iter()
        .map(|(window, workspace, _)| {
            let mut window = window.clone();
            // Tiled views never overlap; floating views may cover anything on their workspace.
            window.overlapping = views
                .iter()
                .filter(|(other, other_ws, other_floating)| *other_floating && other_ws == workspace && other.visible && *other != window && intersects(other.rect, window.rect))
                .map(|(other, _, _)| other.rect)
                .collect();
            window
        })
        .collect()
}

fn hyprland_windows(clients: &Value, active: Option<u64>) -> Vec<Window> {
    clients
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|client| {
            let pair = |name: &str| client.get(name).and_then(Value::as_array).filter(|v| v.len() == 2).map(|v| (v[0].as_i64().unwrap_or(0), v[1].as_i64().unwrap_or(0)));
            let ((x, y), (w, h)) = pair("at").zip(pair("size"))?;
            let address = client.get("address").and_then(Value::as_str).and_then(|a| u64::from_str_radix(a.trim_start_matches("0x"), 16).ok());
            Some(Window {
                pid: client.get("pid").and_then(Value::as_u64)? as u32,
                title: client.get("title").and_then(Value::as_str).unwrap_or_default().to_owned(),
                rect: [x, y, w, h],
                focused: address.is_some() && address == active,
                visible: client.get("mapped").and_then(Value::as_bool) == Some(true) && client.get("hidden").and_then(Value::as_bool) != Some(true),
                address,
                output: None,
                output_rect: None,
                overlapping: Vec::new(),
            })
        })
        .collect()
}

/// Every window the compositor's IPC lists; `Ok(None)` without a known IPC.
pub fn windows() -> Result<Option<Vec<Window>>, Failure> {
    let unavailable = |e: String| failure("TARGET_UNAVAILABLE", format!("Compositor IPC failed: {e}"));
    if let Ok(path) = std::env::var("SWAYSOCK") {
        return Ok(Some(sway_windows(&wlr::sway_request(&path, 4).map_err(unavailable)?)));
    }
    if std::env::var("HYPRLAND_INSTANCE_SIGNATURE").is_ok() {
        let clients = wlr::hyprland_request("j/clients").map_err(unavailable)?;
        let active = wlr::hyprland_request("j/activewindow").map_err(unavailable)?;
        let active = active.get("address").and_then(Value::as_str).and_then(|a| u64::from_str_radix(a.trim_start_matches("0x"), 16).ok());
        return Ok(Some(hyprland_windows(&clients, active)));
    }
    Ok(None)
}

/// The one window of pid titled title; several matches are refused, as on X11.
pub fn find(pid: u32, title: &str) -> Result<Window, Failure> {
    let windows = windows()?.ok_or_else(|| failure("UNSUPPORTED_CAPABILITY", "This compositor has no window IPC the helper knows"))?;
    let mut matches = windows.into_iter().filter(|w| w.pid == pid && w.title == title);
    let window = matches.next().ok_or_else(|| failure("TARGET_UNAVAILABLE", "The compositor lists no window with this process and title"))?;
    if matches.next().is_some() {
        return Err(failure("TARGET_UNAVAILABLE", "Several windows share this process and title"));
    }
    Ok(window)
}

pub fn describe(window: &Window) -> Value {
    json!({ "pid": window.pid, "title": window.title, "rect": window.rect, "focused": window.focused, "visible": window.visible })
}

/// Pixels of one window: 32-bit little-endian XRGB/ARGB rows.
pub struct Image {
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub format: u32,
    pub y_invert: bool,
    pub data: Vec<u8>,
}

impl Image {
    pub fn to_json(&self) -> Value {
        json!({ "width": self.width, "height": self.height, "stride": self.stride, "format": self.format,
            "yInvert": self.y_invert, "data": base64::engine::general_purpose::STANDARD.encode(&self.data) })
    }
}

#[derive(Default)]
struct Capture {
    buffer: Option<(u32, u32, u32, u32)>, // format, width, height, stride
    buffer_done: bool,
    ready: bool,
    failed: bool,
    y_invert: bool,
    outputs: Vec<(wl_output::WlOutput, Option<String>)>,
    toplevels: Vec<(toplevel::ZwlrForeignToplevelHandleV1, String)>,
}

impl Dispatch<wl_registry::WlRegistry, GlobalListContents> for Capture {
    fn event(_: &mut Self, _: &wl_registry::WlRegistry, _: wl_registry::Event, _: &GlobalListContents, _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_shm::WlShm, ()> for Capture {
    fn event(_: &mut Self, _: &wl_shm::WlShm, _: wl_shm::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_shm_pool::WlShmPool, ()> for Capture {
    fn event(_: &mut Self, _: &wl_shm_pool::WlShmPool, _: wl_shm_pool::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_buffer::WlBuffer, ()> for Capture {
    fn event(_: &mut Self, _: &wl_buffer::WlBuffer, _: wl_buffer::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_output::WlOutput, ()> for Capture {
    fn event(state: &mut Self, output: &wl_output::WlOutput, event: wl_output::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        if let wl_output::Event::Name { name } = event {
            if let Some(entry) = state.outputs.iter_mut().find(|(o, _)| o == output) {
                entry.1 = Some(name);
            }
        }
    }
}
impl Dispatch<toplevel_manager::ZwlrForeignToplevelManagerV1, ()> for Capture {
    fn event(state: &mut Self, _: &toplevel_manager::ZwlrForeignToplevelManagerV1, event: toplevel_manager::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        if let toplevel_manager::Event::Toplevel { toplevel } = event {
            state.toplevels.push((toplevel, String::new()));
        }
    }
    wayland_client::event_created_child!(Capture, toplevel_manager::ZwlrForeignToplevelManagerV1, [
        toplevel_manager::EVT_TOPLEVEL_OPCODE => (toplevel::ZwlrForeignToplevelHandleV1, ()),
    ]);
}
impl Dispatch<toplevel::ZwlrForeignToplevelHandleV1, ()> for Capture {
    fn event(state: &mut Self, handle: &toplevel::ZwlrForeignToplevelHandleV1, event: toplevel::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        if let toplevel::Event::Title { title } = event {
            if let Some(entry) = state.toplevels.iter_mut().find(|(h, _)| h == handle) {
                entry.1 = title;
            }
        }
    }
}
impl Dispatch<export_manager::HyprlandToplevelExportManagerV1, ()> for Capture {
    fn event(_: &mut Self, _: &export_manager::HyprlandToplevelExportManagerV1, _: export_manager::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<export_frame::HyprlandToplevelExportFrameV1, ()> for Capture {
    fn event(state: &mut Self, _: &export_frame::HyprlandToplevelExportFrameV1, event: export_frame::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        match event {
            export_frame::Event::Buffer { format: WEnum::Value(format), width, height, stride } => state.buffer = Some((format as u32, width, height, stride)),
            export_frame::Event::BufferDone => state.buffer_done = true,
            export_frame::Event::Flags { flags: WEnum::Value(flags) } => state.y_invert = flags.contains(export_frame::Flags::YInvert),
            export_frame::Event::Ready { .. } => state.ready = true,
            export_frame::Event::Failed => state.failed = true,
            _ => {}
        }
    }
}
impl Dispatch<zwlr_screencopy_manager_v1::ZwlrScreencopyManagerV1, ()> for Capture {
    fn event(_: &mut Self, _: &zwlr_screencopy_manager_v1::ZwlrScreencopyManagerV1, _: zwlr_screencopy_manager_v1::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<zwlr_screencopy_frame_v1::ZwlrScreencopyFrameV1, ()> for Capture {
    fn event(state: &mut Self, _: &zwlr_screencopy_frame_v1::ZwlrScreencopyFrameV1, event: zwlr_screencopy_frame_v1::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        match event {
            zwlr_screencopy_frame_v1::Event::Buffer { format: WEnum::Value(format), width, height, stride } => state.buffer = Some((format as u32, width, height, stride)),
            zwlr_screencopy_frame_v1::Event::BufferDone => state.buffer_done = true,
            zwlr_screencopy_frame_v1::Event::Flags { flags: WEnum::Value(flags) } => state.y_invert = flags.contains(zwlr_screencopy_frame_v1::Flags::YInvert),
            zwlr_screencopy_frame_v1::Event::Ready { .. } => state.ready = true,
            zwlr_screencopy_frame_v1::Event::Failed => state.failed = true,
            _ => {}
        }
    }
}

fn wait(queue: &mut EventQueue<Capture>, state: &mut Capture, done: impl Fn(&Capture) -> bool) -> Result<(), Failure> {
    let deadline = Instant::now() + Duration::from_secs(5);
    while !done(state) {
        if Instant::now() > deadline {
            return Err(failure("CAPTURE_FAILED", "The compositor did not deliver the window image"));
        }
        queue.roundtrip(state).map_err(|e| failure("CAPTURE_FAILED", format!("Wayland connection lost: {e}")))?;
        if state.failed {
            return Err(failure("CAPTURE_FAILED", "The compositor could not capture the window"));
        }
    }
    Ok(())
}

/// Copies a frame offered as wl_shm into memory, once the compositor described it.
fn copy_shm(queue: &mut EventQueue<Capture>, state: &mut Capture, shm: &wl_shm::WlShm, request: impl FnOnce(&wl_buffer::WlBuffer)) -> Result<Image, Failure> {
    let (format, width, height, stride) = state.buffer.ok_or_else(|| failure("CAPTURE_FAILED", "The compositor offered no shared-memory buffer"))?;
    if format > 1 {
        return Err(failure("CAPTURE_FAILED", format!("Unsupported pixel format {format}")));
    }
    let size = stride as usize * height as usize;
    if width == 0 || height == 0 || size > 64 << 20 {
        return Err(failure("CAPTURE_FAILED", "Window image size is unavailable or too large"));
    }
    let fd = unsafe { libc::memfd_create(c"ocu-capture".as_ptr(), libc::MFD_CLOEXEC) };
    if fd < 0 {
        return Err(failure("CAPTURE_FAILED", "Cannot create a capture buffer"));
    }
    let file = std::fs::File::from(unsafe { OwnedFd::from_raw_fd(fd) });
    file.set_len(size as u64).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    let handle = queue.handle();
    let pool = shm.create_pool(file.as_fd(), size as i32, &handle, ());
    let buffer = pool.create_buffer(0, width as i32, height as i32, stride as i32, wl_shm::Format::try_from(format).unwrap_or(wl_shm::Format::Xrgb8888), &handle, ());
    request(&buffer);
    let result = wait(queue, state, |s| s.ready);
    buffer.destroy();
    pool.destroy();
    result?;
    let mut data = Vec::with_capacity(size);
    (&file).read_to_end(&mut data).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    Ok(Image { width, height, stride, format, y_invert: state.y_invert, data })
}

/// Captures one window without activating it.
pub fn capture(window: &Window) -> Result<Image, Failure> {
    if !window.visible {
        return Err(failure("CAPTURE_FAILED", "The window is not visible"));
    }
    let connection = Connection::connect_to_env().map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    let (globals, mut queue) = registry_queue_init::<Capture>(&connection).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    let handle = queue.handle();
    let mut state = Capture::default();
    let shm: wl_shm::WlShm = globals.bind(&handle, 1..=1, ()).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    if let Some(address) = window.address {
        let manager: export_manager::HyprlandToplevelExportManagerV1 =
            globals.bind(&handle, 1..=2, ()).map_err(|_| failure("UNSUPPORTED_CAPABILITY", "The compositor offers no window export"))?;
        // Version 2 names the window by its foreign-toplevel handle; match it by title,
        // which IPC already tied to the target process. Older versions take the low 32
        // bits of the window address.
        let frame = if manager.version() >= 2 {
            let toplevels: toplevel_manager::ZwlrForeignToplevelManagerV1 =
                globals.bind(&handle, 1..=3, ()).map_err(|_| failure("UNSUPPORTED_CAPABILITY", "The compositor offers no toplevel list"))?;
            queue.roundtrip(&mut state).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
            queue.roundtrip(&mut state).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
            let matches: Vec<_> = state.toplevels.iter().filter(|(_, title)| *title == window.title).map(|(h, _)| h.clone()).collect();
            if matches.len() != 1 {
                return Err(failure("CAPTURE_FAILED", format!("{} toplevels are titled like the target", matches.len())));
            }
            let frame = manager.capture_toplevel_with_wlr_toplevel_handle(0, &matches[0], &handle, ());
            let _ = toplevels;
            frame
        } else {
            manager.capture_toplevel(0, (address & 0xffff_ffff) as u32, &handle, ())
        };
        wait(&mut queue, &mut state, |s| s.buffer_done || (s.buffer.is_some() && s.ready))?;
        let image = copy_shm(&mut queue, &mut state, &shm, |buffer| frame.copy(buffer, 1));
        frame.destroy();
        return image;
    }
    // Sway: copy the window's region of its output, unless something may cover it.
    if !window.overlapping.is_empty() {
        return Err(failure("CAPTURE_FAILED", "A floating window overlaps the target; an output region would show it"));
    }
    let manager: zwlr_screencopy_manager_v1::ZwlrScreencopyManagerV1 =
        globals.bind(&handle, 1..=3, ()).map_err(|_| failure("UNSUPPORTED_CAPABILITY", "The compositor offers no screencopy"))?;
    for global in globals.contents().clone_list().into_iter().filter(|g| g.interface == "wl_output") {
        let output: wl_output::WlOutput = globals.registry().bind(global.name, global.version.min(4), &handle, ());
        state.outputs.push((output, None));
    }
    queue.roundtrip(&mut state).map_err(|e| failure("CAPTURE_FAILED", e.to_string()))?;
    let wanted = window.output.clone().unwrap_or_default();
    let output = state.outputs.iter().find(|(_, name)| name.as_deref() == Some(&wanted)).map(|(o, _)| o.clone()).ok_or_else(|| failure("CAPTURE_FAILED", "The window's output is not available"))?;
    let origin = window.output_rect.ok_or_else(|| failure("CAPTURE_FAILED", "The window's output geometry is unknown"))?;
    let [x, y, w, h] = window.rect;
    let frame = manager.capture_output_region(0, &output, (x - origin[0]) as i32, (y - origin[1]) as i32, w as i32, h as i32, &handle, ());
    wait(&mut queue, &mut state, |s| s.buffer_done || s.buffer.is_some())?;
    let image = copy_shm(&mut queue, &mut state, &shm, |buffer| frame.copy(buffer));
    frame.destroy();
    image
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sway_tree_reports_overlapping_floating_windows() {
        let tree = json!({"type": "root", "nodes": [{"type": "output", "name": "HEADLESS-1", "rect": {"x": 0, "y": 0, "width": 1024, "height": 768}, "nodes": [
            {"type": "workspace", "name": "1", "nodes": [
                {"type": "con", "pid": 10, "name": "Tiled", "focused": true, "visible": true, "rect": {"x": 0, "y": 0, "width": 512, "height": 768}}],
             "floating_nodes": [
                {"type": "floating_con", "pid": 11, "name": "Dialog", "visible": true, "rect": {"x": 400, "y": 100, "width": 300, "height": 200}}]}]}]});
        let windows = sway_windows(&tree);
        let tiled = windows.iter().find(|w| w.title == "Tiled").unwrap();
        assert_eq!(tiled.output.as_deref(), Some("HEADLESS-1"));
        assert_eq!(tiled.overlapping, vec![[400, 100, 300, 200]]);
        assert!(windows.iter().find(|w| w.title == "Dialog").unwrap().overlapping.is_empty());
    }

    #[test]
    fn hyprland_clients_carry_address_and_focus() {
        let clients = json!([{"address": "0x55d1a2b3c4d0", "mapped": true, "hidden": false, "at": [21, 21], "size": [982, 726], "pid": 7, "title": "App"}]);
        let windows = hyprland_windows(&clients, Some(0x55d1a2b3c4d0));
        assert_eq!(windows[0].rect, [21, 21, 982, 726]);
        assert_eq!(windows[0].address, Some(0x55d1a2b3c4d0));
        assert!(windows[0].focused && windows[0].visible);
    }
}
