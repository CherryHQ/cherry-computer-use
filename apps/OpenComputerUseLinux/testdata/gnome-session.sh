#!/bin/sh
# Run a command in an isolated headless GNOME Shell (Wayland) with its own D-Bus
# session and AT-SPI bus. Nothing is shown on, or sent to, the desktop in use.
# OPEN_COMPUTER_USE_WAYLAND_TEST=1 lets the Wayland helper use Mutter's
# RemoteDesktop API in place of the portal dialog, which no one can click here.
#
#   testdata/gnome-session.sh go test ./internal/desktop/ -run GNOME
set -eu
if [ "${OCU_GNOME_SESSION_INNER:-}" != 1 ]; then
  # Everything the shell and its services write goes under a private root: the
  # runtime directory (Wayland, AT-SPI and other sockets), config, data, cache and
  # state. Sharing the desktop's directories lets the test session replace its
  # sockets, for example the accessibility bus, and its saved shell state.
  root=$(mktemp -d "${TMPDIR:-/tmp}/ocu-gnome-session.XXXXXX")
  for dir in run config data cache state; do mkdir -m 700 "$root/$dir"; done
  if command -v go >/dev/null 2>&1; then
    GOCACHE=$(go env GOCACHE)
    export GOCACHE
  fi
  exec env -u DBUS_SESSION_BUS_ADDRESS -u DISPLAY -u WAYLAND_DISPLAY -u AT_SPI_BUS_ADDRESS -u XAUTHORITY \
    XDG_RUNTIME_DIR="$root/run" XDG_CONFIG_HOME="$root/config" XDG_DATA_HOME="$root/data" \
    XDG_CACHE_HOME="$root/cache" XDG_STATE_HOME="$root/state" \
    OCU_GNOME_SESSION_INNER=1 OCU_GNOME_SESSION_ROOT="$root" dbus-run-session -- "$0" "$@"
fi
case "${OCU_GNOME_SESSION_ROOT:-}" in
  "${TMPDIR:-/tmp}"/ocu-gnome-session.*) ;;
  *) echo "gnome-session.sh: missing private session root" >&2; exit 1 ;;
esac
display="wayland-ocu-test"
log="$OCU_GNOME_SESSION_ROOT/shell.log"
gnome-shell --headless --wayland --no-x11 --wayland-display="$display" --virtual-monitor 1024x768 >"$log" 2>&1 &
shell=$!
cleanup() {
  kill "$shell" 2>/dev/null || true
  wait "$shell" 2>/dev/null || true
  rm -rf "$OCU_GNOME_SESSION_ROOT"
  # Session services can still recreate their runtime directory while exiting.
  (for _ in 1 2 3 4 5; do sleep 1; rm -rf "$OCU_GNOME_SESSION_ROOT"; done) &
}
trap cleanup EXIT
for _ in $(seq 100); do
  gdbus introspect --session --dest org.gnome.Mutter.RemoteDesktop --object-path /org/gnome/Mutter/RemoteDesktop >/dev/null 2>&1 && break
  sleep 0.1
done
launcher=$(command -v at-spi-bus-launcher || echo /usr/libexec/at-spi-bus-launcher)
"$launcher" --launch-immediately >/dev/null 2>&1 &
sleep 1
# A headless shell has no keyboard, so no window can take focus. Give the seat
# one for the whole run, as a physical keyboard does on a real desktop, by
# holding a helper's EIS keyboard open (OPEN_COMPUTER_USE_WAYLAND_HELPER).
if [ -n "${OPEN_COMPUTER_USE_WAYLAND_HELPER:-}" ]; then
  mkfifo "$log.keyboard"
  python3 -c '
import json, sys, time
def call(method, **params):
    body = json.dumps({"id": 1, "method": method, "params": params}).encode()
    sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    sys.stdout.buffer.flush()
call("hello", protocol=1)
call("keyboard.grant", via="mutter-test")
open(sys.argv[1]).read()
' "$log.keyboard" | OPEN_COMPUTER_USE_WAYLAND_TEST=1 "$OPEN_COMPUTER_USE_WAYLAND_HELPER" >/dev/null &
  keyboard=$!
  trap 'echo > "$log.keyboard"; cleanup' EXIT
  sleep 1
fi
env WAYLAND_DISPLAY="$display" XDG_SESSION_TYPE=wayland GDK_BACKEND=wayland \
  OPEN_COMPUTER_USE_WAYLAND_TEST=1 OPEN_COMPUTER_USE_LINUX_GNOME_TEST=1 "$@"
