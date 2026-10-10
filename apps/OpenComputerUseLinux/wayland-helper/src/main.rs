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
mod probe;

use std::io::{self, BufReader, Write};

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

fn main() {
    let stdin = io::stdin();
    let mut input = BufReader::new(stdin.lock());
    let mut output = io::stdout().lock();
    let code = match serve(&mut input, &mut output) {
        Ok(()) => 0,
        Err(error) => {
            eprintln!("open-computer-use-wayland: {error}");
            2
        }
    };
    release_all();
    std::process::exit(code);
}

/// Serves requests until end of input. A malformed frame ends the session: the
/// runtime treats the helper as gone rather than guessing where frames resume.
fn serve(input: &mut impl io::BufRead, output: &mut impl Write) -> io::Result<()> {
    while let Some(body) = frame::read(input)? {
        let request: Request = serde_json::from_slice(&body).map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
        let response = match handle(&request) {
            Ok(result) => json!({ "id": request.id, "result": result }),
            Err((code, message)) => json!({ "id": request.id, "error": { "code": code, "message": message, "effect": "none" } }),
        };
        frame::write(output, response.to_string().as_bytes())?;
    }
    Ok(())
}

fn handle(request: &Request) -> Result<Value, (&'static str, String)> {
    match request.method.as_str() {
        "hello" => {
            let requested = request.params.get("protocol").and_then(Value::as_u64);
            if requested != Some(PROTOCOL) {
                return Err(("INVALID_ARGUMENT", format!("helper speaks protocol {PROTOCOL}, runtime asked for {requested:?}")));
            }
            Ok(json!({ "protocol": PROTOCOL, "version": env!("CARGO_PKG_VERSION"), "probe": probe::run() }))
        }
        "probe" => Ok(json!(probe::run())),
        other => Err(("UNSUPPORTED_CAPABILITY", format!("unknown helper method {other:?}"))),
    }
}

/// Releases held input and closes portal sessions. Nothing is held yet; input
/// backends register their cleanup here as they land.
fn release_all() {}
