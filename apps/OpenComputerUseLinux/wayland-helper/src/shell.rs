//! Client of the computer-use@cherryai GNOME Shell extension
//! (gnome-shell-extension/). The extension identifies this helper by its
//! executable and serves nothing until the user allows it in a Shell dialog; every
//! request names the window by Mutter id plus the observed PID and title, and the
//! extension checks the target in the same call that sends input.

use std::{sync::OnceLock, time::Duration};

use serde_json::{Value, json};
use zbus::blocking::{Connection, connection::Builder, fdo::DBusProxy};

use crate::keyboard::Failure;

const NAME: &str = "com.cherryai.ComputerUse.Shell";
const PATH: &str = "/com/cherryai/ComputerUse/Shell";
const INTERFACE: &str = "com.cherryai.ComputerUse.Shell1";
/// The extension interface this helper speaks.
pub const PROTOCOL: u32 = 1;

fn failure(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// zbus times out per connection, so the authorization dialog, which waits for
/// the user, gets a connection of its own. Both come from this process, which the
/// extension identifies by executable.
fn connection(waits_for_user: bool) -> Result<&'static Connection, Failure> {
    static QUICK: OnceLock<Connection> = OnceLock::new();
    static PATIENT: OnceLock<Connection> = OnceLock::new();
    let (cell, timeout) = if waits_for_user { (&PATIENT, 600) } else { (&QUICK, 15) };
    if let Some(connection) = cell.get() {
        return Ok(connection);
    }
    let connection = Builder::session()
        .and_then(|builder| builder.method_timeout(Duration::from_secs(timeout)).build())
        .map_err(|e| failure("DEPENDENCY_MISSING", format!("Cannot reach the session bus: {e}")))?;
    Ok(cell.get_or_init(|| connection))
}

/// The extension's protocol version when it is running, read for the probe.
pub fn running() -> Option<u32> {
    let connection = connection(false).ok()?;
    let owned = DBusProxy::new(connection).ok()?.name_has_owner(NAME.try_into().ok()?).ok()?;
    if !owned {
        return None;
    }
    call::<(u32,)>("GetVersion", &(), false).ok().map(|(version,)| version)
}

/// Calls one extension method. D-Bus errors map onto runtime codes; `Possible`
/// marks a failure after input was sent.
fn call<R: serde::de::DeserializeOwned + zbus::zvariant::Type>(
    method: &str,
    body: &(impl serde::Serialize + zbus::zvariant::DynamicType),
    waits_for_user: bool,
) -> Result<R, Failure> {
    let reply =
        connection(waits_for_user)?.call_method(Some(NAME), PATH, Some(INTERFACE), method, body).map_err(refusal)?;
    reply
        .body()
        .deserialize::<R>()
        .map_err(|e| failure("TARGET_UNAVAILABLE", format!("Unexpected reply from the GNOME Shell extension: {e}")))
}

fn refusal(error: zbus::Error) -> Failure {
    let zbus::Error::MethodError(name, description, _) = &error else {
        return failure("TARGET_UNAVAILABLE", format!("GNOME Shell extension call failed: {error}"));
    };
    let message = description.clone().unwrap_or_default();
    let name = name.as_str();
    let code = match name.rsplit('.').next().unwrap_or_default() {
        "PermissionRequired" => "PERMISSION_REQUIRED",
        "InvalidArgument" => "INVALID_ARGUMENT",
        "UnsupportedCapability" => "UNSUPPORTED_CAPABILITY",
        "CaptureFailed" => "CAPTURE_FAILED",
        "Possible" => return Failure { code: "TARGET_UNAVAILABLE", message, sent: true },
        _ if name.starts_with("org.freedesktop.DBus.Error.") => {
            // The name is gone: the screen is locked (GNOME disables extensions
            // there) or the extension was disabled.
            return failure(
                "TARGET_UNAVAILABLE",
                format!("The GNOME Shell extension is not running; the screen may be locked ({message})"),
            );
        }
        _ => "TARGET_UNAVAILABLE",
    };
    failure(code, message)
}

/// `granted`, `denied` or `notDetermined` for this helper's executable.
pub fn authorization() -> Result<String, Failure> {
    call::<(String,)>("GetAuthorization", &(), false).map(|(status,)| status)
}

/// Shows the Shell's dialog unless already granted; blocks until the user answers.
pub fn authorize() -> Result<String, Failure> {
    call::<(String,)>("Authorize", &(), true).map(|(status,)| status)
}

/// The extension's window list, bottom of the stack first.
pub fn windows() -> Result<Vec<Value>, Failure> {
    let (json,) = call::<(String,)>("ListWindows", &(), false)?;
    serde_json::from_str(&json).map_err(|e| failure("TARGET_UNAVAILABLE", format!("Unreadable window list: {e}")))
}

pub struct Target<'a> {
    pub id: u32,
    pub pid: u32,
    pub title: &'a str,
}

/// PNG of the window's frame at its resource scale, with its pixel size.
pub fn capture(target: &Target) -> Result<(Vec<u8>, u32, u32), Failure> {
    call("CaptureWindow", &(target.id, target.pid, target.title), false)
}

pub fn activate(target: &Target) -> Result<bool, Failure> {
    call::<(bool,)>("Activate", &(target.id, target.pid, target.title), false).map(|(focused,)| focused)
}

pub fn click(target: &Target, x: f64, y: f64, button: u32, count: u32) -> Result<(), Failure> {
    call("Click", &(target.id, target.pid, target.title, x, y, button, count), false)
}

pub fn drag(target: &Target, from: (f64, f64), to: (f64, f64)) -> Result<(), Failure> {
    call("Drag", &(target.id, target.pid, target.title, from.0, from.1, to.0, to.1), false)
}

pub fn key_chord(target: &Target, modifiers: &[u32], key: u32) -> Result<(), Failure> {
    call("KeyChord", &(target.id, target.pid, target.title, modifiers, key), false)
}

pub fn text(target: &Target, text: &str) -> Result<(), Failure> {
    call("Text", &(target.id, target.pid, target.title, text), false)
}

/// The probe's view of the extension.
pub fn probe() -> Value {
    match running() {
        Some(protocol) => json!({ "protocol": protocol }),
        None => Value::Null,
    }
}
