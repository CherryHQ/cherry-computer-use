//! Wayland display helper for the Linux runtime.
//!
//! The Go runtime starts one helper per runtime in a Wayland session and talks to
//! it over stdin/stdout with Content-Length framed JSON:
//! `{"id":1,"method":"hello","params":{"protocol":1}}` →
//! `{"id":1,"result":{...}}` or `{"id":1,"error":{"code","message","effect"}}`.
//! Native handles stay here. End of input means the runtime closed or died: the
//! helper releases what it holds and exits. See
//! docs/design-docs/linux-display-backends.md.

mod frame;
mod keyboard;
mod keymap;
mod probe;
mod wlr;

use std::io::{self, BufReader, Write};

use keyboard::Failure;
use serde::Deserialize;
use serde_json::{Value, json};

const PROTOCOL: u64 = 1;

#[derive(Deserialize)]
struct Request {
    id: u64,
    method: String,
    #[serde(default)]
    params: Value,
}

/// State owned for the runtime; dropping it releases input and closes sessions.
#[derive(Default)]
struct Helper {
    keyboard: Option<keyboard::Keyboard>,
    virtual_keyboard: Option<wlr::VirtualKeyboard>,
}

fn main() {
    let stdin = io::stdin();
    let mut input = BufReader::new(stdin.lock());
    let mut output = io::stdout().lock();
    let mut helper = Helper::default();
    let code = match serve(&mut helper, &mut input, &mut output) {
        Ok(()) => 0,
        Err(error) => {
            eprintln!("open-computer-use-wayland: {error}");
            2
        }
    };
    // End of input: release held keys and close the remote-desktop session.
    drop(helper);
    std::process::exit(code);
}

/// Serves requests until end of input. A malformed frame ends the session: the
/// runtime treats the helper as gone rather than guessing where frames resume.
fn serve(helper: &mut Helper, input: &mut impl io::BufRead, output: &mut impl Write) -> io::Result<()> {
    while let Some(body) = frame::read(input)? {
        let request: Request = serde_json::from_slice(&body).map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
        let response = match handle(helper, &request) {
            Ok(result) => json!({ "id": request.id, "result": result }),
            Err(failure) => json!({ "id": request.id, "error": {
                "code": failure.code, "message": failure.message, "effect": if failure.sent { "possible" } else { "none" } } }),
        };
        frame::write(output, response.to_string().as_bytes())?;
    }
    Ok(())
}

fn refuse(code: &'static str, message: impl Into<String>) -> Failure {
    Failure { code, message: message.into(), sent: false }
}

/// The Mutter route skips the consent dialog, so it is accepted only when the
/// isolated test session opts in explicitly.
fn test_session() -> bool {
    std::env::var("OPEN_COMPUTER_USE_WAYLAND_TEST").as_deref() == Ok("1")
}

fn handle(helper: &mut Helper, request: &Request) -> Result<Value, Failure> {
    let text = |name: &str| request.params.get(name).and_then(Value::as_str).ok_or_else(|| refuse("INVALID_ARGUMENT", format!("{name} is required")));
    let keysym = |value: &Value| value.as_u64().and_then(|v| u32::try_from(v).ok()).ok_or_else(|| refuse("INVALID_ARGUMENT", "keysyms are unsigned 32-bit numbers"));
    match request.method.as_str() {
        "hello" => {
            let requested = request.params.get("protocol").and_then(Value::as_u64);
            if requested != Some(PROTOCOL) {
                return Err(refuse("INVALID_ARGUMENT", format!("helper speaks protocol {PROTOCOL}, runtime asked for {requested:?}")));
            }
            Ok(json!({ "protocol": PROTOCOL, "version": env!("CARGO_PKG_VERSION"), "probe": probe::run() }))
        }
        "probe" => Ok(json!(probe::run())),
        "keyboard.status" => Ok(json!({ "granted": helper.keyboard.is_some() })),
        "keyboard.grant" => {
            if helper.keyboard.is_none() {
                helper.keyboard = Some(match request.params.get("via").and_then(Value::as_str) {
                    None | Some("portal") => keyboard::Keyboard::grant_portal()?,
                    Some("mutter-test") if test_session() => keyboard::Keyboard::grant_mutter_for_test()?,
                    Some(other) => return Err(refuse("INVALID_ARGUMENT", format!("unknown grant route {other:?}"))),
                });
            }
            Ok(json!({ "granted": true }))
        }
        "window.active" => match wlr::active_window()? {
            None => Ok(json!({ "available": false })),
            Some(window) => Ok(json!({ "available": true, "pid": window.pid, "title": window.title, "rect": window.rect })),
        },
        method if method.starts_with("vkeyboard.") => {
            if helper.virtual_keyboard.is_none() {
                helper.virtual_keyboard = Some(wlr::VirtualKeyboard::connect()?);
            }
            let keyboard = helper.virtual_keyboard.as_mut().unwrap();
            let result = match method {
                "vkeyboard.prepare" => Ok(json!({})),
                "vkeyboard.chord" => {
                    let modifiers = request.params.get("modifiers").and_then(Value::as_array).cloned().unwrap_or_default();
                    let modifiers = modifiers.iter().map(keysym).collect::<Result<Vec<_>, _>>()?;
                    let key = keysym(request.params.get("key").unwrap_or(&Value::Null))?;
                    keyboard.chord(&modifiers, key).map(|()| json!({}))
                }
                "vkeyboard.text" => keyboard.text(text("text")?).map(|()| json!({})),
                "vkeyboard.hold" if test_session() => keyboard.hold_for_test(keysym(request.params.get("key").unwrap_or(&Value::Null))?).map(|()| json!({})),
                other => return Err(refuse("UNSUPPORTED_CAPABILITY", format!("unknown helper method {other:?}"))),
            };
            // A lost Wayland connection takes the device with it; create a new one next time.
            if result.as_ref().is_err_and(|f| f.message.contains("Wayland connection lost")) {
                helper.virtual_keyboard = None;
            }
            result
        }
        method if method.starts_with("keyboard.") => {
            let keyboard = helper.keyboard.as_mut().ok_or_else(|| refuse("PERMISSION_REQUIRED", "Remote keyboard input has not been granted"))?;
            let result = match method {
                "keyboard.missing" => keyboard.missing(text("text")?).map(|missing| json!({ "missing": missing.iter().map(char::to_string).collect::<Vec<_>>() })),
                "keyboard.chord" => {
                    let modifiers = request.params.get("modifiers").and_then(Value::as_array).cloned().unwrap_or_default();
                    let modifiers = modifiers.iter().map(keysym).collect::<Result<Vec<_>, _>>()?;
                    let key = keysym(request.params.get("key").unwrap_or(&Value::Null))?;
                    keyboard.chord(&modifiers, key).map(|()| json!({}))
                }
                "keyboard.text" => keyboard.text(text("text")?).map(|()| json!({})),
                "keyboard.hold" if test_session() => keyboard.hold_for_test(keysym(request.params.get("key").unwrap_or(&Value::Null))?).map(|()| json!({})),
                other => return Err(refuse("UNSUPPORTED_CAPABILITY", format!("unknown helper method {other:?}"))),
            };
            // A lost EIS connection means the grant is gone; ask again rather than reuse it.
            if result.as_ref().is_err_and(|f| f.code == "TARGET_UNAVAILABLE" && f.message.contains("EIS")) {
                helper.keyboard = None;
            }
            result
        }
        other => Err(refuse("UNSUPPORTED_CAPABILITY", format!("unknown helper method {other:?}"))),
    }
}
