//! Session probe: what this Wayland session offers, read without side effects.
//! The Go runtime derives its capability report from this.

use std::{env, time::Duration};

use serde::Serialize;
use wayland_client::{
    Connection, Dispatch, QueueHandle,
    globals::{GlobalListContents, registry_queue_init},
    protocol::wl_registry,
};

#[derive(Serialize)]
pub struct Probe {
    pub session: Session,
    pub wayland: Wayland,
    pub portal: Portal,
    pub compositor: Compositor,
}

#[derive(Serialize)]
pub struct Session {
    #[serde(rename = "type")]
    pub kind: Option<String>,
    pub wayland_display: Option<String>,
    pub x11_display: Option<String>,
    pub desktop: Option<String>,
}

#[derive(Serialize, Default)]
pub struct Wayland {
    pub connected: bool,
    pub globals: Vec<Global>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

#[derive(Serialize)]
pub struct Global {
    pub interface: String,
    pub version: u32,
}

#[derive(Serialize, Default)]
pub struct Portal {
    pub remote_desktop: Option<PortalInterface>,
    pub screencast: Option<PortalInterface>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// `types` is AvailableDeviceTypes for RemoteDesktop and AvailableSourceTypes for ScreenCast.
#[derive(Serialize)]
pub struct PortalInterface {
    pub version: u32,
    pub types: u32,
}

#[derive(Serialize)]
pub struct Compositor {
    pub sway: bool,
    pub hyprland: bool,
}

pub fn run() -> Probe {
    let var = |name: &str| env::var(name).ok().filter(|value| !value.is_empty());
    Probe {
        session: Session {
            kind: var("XDG_SESSION_TYPE"),
            wayland_display: var("WAYLAND_DISPLAY"),
            x11_display: var("DISPLAY"),
            desktop: var("XDG_CURRENT_DESKTOP"),
        },
        wayland: wayland(),
        portal: portal(),
        compositor: Compositor { sway: var("SWAYSOCK").is_some(), hyprland: var("HYPRLAND_INSTANCE_SIGNATURE").is_some() },
    }
}

struct Registry;

impl Dispatch<wl_registry::WlRegistry, GlobalListContents> for Registry {
    fn event(_: &mut Self, _: &wl_registry::WlRegistry, _: wl_registry::Event, _: &GlobalListContents, _: &Connection, _: &QueueHandle<Self>) {}
}

fn wayland() -> Wayland {
    let connection = match Connection::connect_to_env() {
        Ok(connection) => connection,
        Err(error) => return Wayland { error: Some(error.to_string()), ..Default::default() },
    };
    match registry_queue_init::<Registry>(&connection) {
        Ok((globals, _queue)) => {
            let mut list: Vec<Global> = globals
                .contents()
                .clone_list()
                .into_iter()
                .map(|global| Global { interface: global.interface, version: global.version })
                .collect();
            list.sort_by(|a, b| a.interface.cmp(&b.interface));
            Wayland { connected: true, globals: list, error: None }
        }
        Err(error) => Wayland { error: Some(error.to_string()), ..Default::default() },
    }
}

fn portal() -> Portal {
    let connection = match zbus::blocking::connection::Builder::session().and_then(|builder| builder.method_timeout(Duration::from_secs(3)).build()) {
        Ok(connection) => connection,
        Err(error) => return Portal { error: Some(error.to_string()), ..Default::default() },
    };
    let read = |interface: &str, types: &str| -> Option<PortalInterface> {
        let proxy = zbus::blocking::Proxy::new(&connection, "org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop", interface).ok()?;
        Some(PortalInterface { version: proxy.get_property("version").ok()?, types: proxy.get_property(types).ok()? })
    };
    Portal {
        remote_desktop: read("org.freedesktop.portal.RemoteDesktop", "AvailableDeviceTypes"),
        screencast: read("org.freedesktop.portal.ScreenCast", "AvailableSourceTypes"),
        error: None,
    }
}
