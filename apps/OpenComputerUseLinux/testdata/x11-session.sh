#!/bin/sh
# Run a command in an isolated X11 desktop: a nested Xephyr (or headless Xvfb)
# display with its own D-Bus session and AT-SPI bus. Input tests use it so that
# synthesized keys and pointer motion never reach the desktop in use.
#
#   testdata/x11-session.sh go test ./internal/desktop/ -run X11
set -eu
display=${X11_SESSION_DISPLAY:-:77}
# The session's services write only under a private root, never into the
# desktop's runtime, config, data, cache or state directories.
root=$(mktemp -d "${TMPDIR:-/tmp}/ocu-x11-session.XXXXXX")
for dir in run config data cache state; do mkdir -m 700 "$root/$dir"; done
if command -v go >/dev/null 2>&1; then
  GOCACHE=$(go env GOCACHE)
  export GOCACHE
fi
if command -v Xvfb >/dev/null 2>&1; then
  Xvfb "$display" -screen 0 900x600x24 -nolisten tcp >/dev/null 2>&1 &
elif command -v Xephyr >/dev/null 2>&1; then
  Xephyr "$display" -screen 900x600 -nolisten tcp >/dev/null 2>&1 &
else
  echo "x11-session.sh needs Xvfb or Xephyr" >&2
  exit 1
fi
server=$!
trap 'kill "$server" 2>/dev/null || true; rm -rf "$root"; (for _ in 1 2 3 4 5; do sleep 1; rm -rf "$root"; done) &' EXIT
socket="/tmp/.X11-unix/X${display#:}"
for _ in $(seq 50); do [ -S "$socket" ] && break; sleep 0.1; done
[ -S "$socket" ] || { echo "X server did not start on $display" >&2; exit 1; }
launcher=$(command -v at-spi-bus-launcher || echo /usr/libexec/at-spi-bus-launcher)
env -u WAYLAND_DISPLAY -u DBUS_SESSION_BUS_ADDRESS -u AT_SPI_BUS_ADDRESS -u XAUTHORITY \
  XDG_RUNTIME_DIR="$root/run" XDG_CONFIG_HOME="$root/config" XDG_DATA_HOME="$root/data" \
  XDG_CACHE_HOME="$root/cache" XDG_STATE_HOME="$root/state" \
  DISPLAY="$display" XDG_SESSION_TYPE=x11 GDK_BACKEND=x11 OPEN_COMPUTER_USE_LINUX_X11_INPUT_TEST=1 \
  dbus-run-session -- sh -c '"$0" --launch-immediately >/dev/null 2>&1 & sleep 0.5; "$@"' "$launcher" "$@"
