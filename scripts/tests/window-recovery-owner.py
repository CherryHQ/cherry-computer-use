"""Own a production recovery child until the native regression sends SIGKILL."""
import json
import pathlib
import signal
import subprocess
import sys
import time

signal.alarm(30)
binary, pid, window_id, ready = sys.argv[1:5]
child = subprocess.Popen(
    [binary, "__computer-use-window-recovery"], stdin=subprocess.PIPE, stdout=subprocess.PIPE
)
def request(message):
    body = json.dumps(message).encode()
    child.stdin.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
    child.stdin.flush()
    header = child.stdout.readline()
    assert header.startswith(b"Content-Length: "), header
    length = int(header.split(b":", 1)[1])
    assert child.stdout.readline() == b"\r\n"
    return json.loads(child.stdout.read(length))

reply = request({"operation": "remember", "pid": int(pid), "windowID": int(window_id)})
result = {"pid": child.pid, "reply": reply}
if sys.argv[5:] == ["--virtual-display"]:
    result["display"] = request({"operation": "createDisplay"})
pathlib.Path(ready).write_text(json.dumps(result))
time.sleep(25)
