use std::{
    io::{BufRead, BufReader, Write},
    process::{Command, Stdio},
    time::{Duration, Instant},
};

fn helper() -> std::process::Child {
    Command::new(env!("CARGO_BIN_EXE_open-computer-use-wayland"))
        // An empty session keeps the probe from touching a real desktop.
        .env_clear()
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .unwrap()
}

fn send(child: &mut std::process::Child, body: &str) {
    write!(child.stdin.as_mut().unwrap(), "Content-Length: {}\r\n\r\n{}", body.len(), body).unwrap();
}

fn receive(reader: &mut impl BufRead) -> serde_json::Value {
    let mut length = 0;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        if line == "\r\n" {
            break;
        }
        if let Some(value) = line.strip_prefix("Content-Length: ") {
            length = value.trim().parse().unwrap();
        }
    }
    let mut body = vec![0; length];
    reader.read_exact(&mut body).unwrap();
    serde_json::from_slice(&body).unwrap()
}

fn wait(child: &mut std::process::Child) -> i32 {
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        if let Some(status) = child.try_wait().unwrap() {
            return status.code().unwrap_or(-1);
        }
        assert!(Instant::now() < deadline, "helper did not exit");
        std::thread::sleep(Duration::from_millis(20));
    }
}

#[test]
fn hello_reports_probe_and_end_of_input_exits_cleanly() {
    let mut child = helper();
    let mut reader = BufReader::new(child.stdout.take().unwrap());
    send(&mut child, r#"{"id":7,"method":"hello","params":{"protocol":1}}"#);
    let response = receive(&mut reader);
    assert_eq!(response["id"], 7);
    assert_eq!(response["result"]["protocol"], 1);
    assert_eq!(response["result"]["probe"]["wayland"]["connected"], false);
    send(&mut child, r#"{"id":8,"method":"teleport"}"#);
    let response = receive(&mut reader);
    assert_eq!(response["error"]["code"], "UNSUPPORTED_CAPABILITY");
    assert_eq!(response["error"]["effect"], "none");
    drop(child.stdin.take());
    assert_eq!(wait(&mut child), 0);
}

#[test]
fn wrong_protocol_is_refused() {
    let mut child = helper();
    let mut reader = BufReader::new(child.stdout.take().unwrap());
    send(&mut child, r#"{"id":1,"method":"hello","params":{"protocol":99}}"#);
    assert_eq!(receive(&mut reader)["error"]["code"], "INVALID_ARGUMENT");
    drop(child.stdin.take());
    assert_eq!(wait(&mut child), 0);
}

#[test]
fn malformed_frame_ends_the_helper() {
    let mut child = helper();
    write!(child.stdin.as_mut().unwrap(), "Content-Length: nope\r\n\r\n").unwrap();
    drop(child.stdin.take());
    assert_ne!(wait(&mut child), 0);
}
