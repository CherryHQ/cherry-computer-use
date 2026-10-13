#!/bin/sh
# Run a command in an isolated headless GNOME Shell (Wayland) with its own D-Bus
# session and AT-SPI bus. Nothing is shown on, or sent to, the desktop in use.
# OPEN_COMPUTER_USE_WAYLAND_TEST=1 lets the Wayland helper use Mutter's
# RemoteDesktop API in place of the portal dialog, which no one can click here.
#
#   testdata/gnome-session.sh go test ./internal/desktop/ -run GNOME
#
# OCU_XWAYLAND=1 lets Mutter run XWayland and gives the command its DISPLAY with
# GDK_BACKEND=x11, so GTK test windows are X11 windows.
#
# OCU_GNOME_SHELL_EXTENSION=<dir> installs that extension directory into the
# session's private data directory and enables it before the shell starts; given
# the open-computer-use executable instead, it runs its install-gnome-extension,
# as a user would before logging in again.
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
if [ -f "${OCU_GNOME_SHELL_EXTENSION:-}" ] && [ -x "$OCU_GNOME_SHELL_EXTENSION" ]; then
  XDG_CURRENT_DESKTOP=GNOME "$OCU_GNOME_SHELL_EXTENSION" install-gnome-extension
elif [ -n "${OCU_GNOME_SHELL_EXTENSION:-}" ]; then
  uuid=$(basename "$OCU_GNOME_SHELL_EXTENSION")
  mkdir -p "$XDG_DATA_HOME/gnome-shell/extensions"
  cp -r "$OCU_GNOME_SHELL_EXTENSION" "$XDG_DATA_HOME/gnome-shell/extensions/$uuid"
  # The private bus activates its own dconf service, writing under XDG_CONFIG_HOME.
  gsettings set org.gnome.shell disable-user-extensions false
  gsettings set org.gnome.shell enabled-extensions "['$uuid']"
fi
# Notification banners can cover test windows; actions there are rightly refused.
gsettings set org.gnome.desktop.notifications show-banners false
# Unsafe mode allows org.gnome.Shell.Eval, used below only inside this session.
# XWayland's sockets live in the shared /tmp/.X11-unix; the new one is ours.
x_before=$(ls /tmp/.X11-unix 2>/dev/null || true)
x11=--no-x11
[ "${OCU_XWAYLAND:-}" = 1 ] && x11=
gnome-shell --headless --unsafe-mode --wayland $x11 --wayland-display="$display" --virtual-monitor 1024x768 >"$log" 2>&1 &
shell=$!
# Session services (document portal, gvfs) may mount FUSE filesystems in the
# private runtime directory. Cleanup must never change the command's exit status.
cleanup() {
  set +e
  kill "$shell" 2>/dev/null
  wait "$shell" 2>/dev/null
  remove_root "$OCU_GNOME_SESSION_ROOT"
}
remove_root() {
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    mounts=$(awk -v root="$1/" 'index($2, root) == 1 { print $2 }' /proc/mounts)
    [ -z "$mounts" ] && break
    for mount in $mounts; do fusermount3 -u -z "$mount" 2>/dev/null || fusermount -u -z "$mount" 2>/dev/null; done
    sleep 0.5
  done
  rm -rf "$1" 2>/dev/null
  # Exiting services can recreate their runtime directories briefly.
  (for _ in 1 2 3 4 5; do sleep 1; rm -rf "$1" 2>/dev/null; done) &
}
trap cleanup EXIT
for _ in $(seq 100); do
  gdbus introspect --session --dest org.gnome.Mutter.RemoteDesktop --object-path /org/gnome/Mutter/RemoteDesktop >/dev/null 2>&1 && break
  sleep 0.1
done
# A login session hands the display to services the bus starts later, such as
# the portal's dialogs; GNOME 42 and 43 do not do it themselves when headless.
gdbus call --session --dest org.freedesktop.DBus --object-path /org/freedesktop/DBus \
  --method org.freedesktop.DBus.UpdateActivationEnvironment "{'WAYLAND_DISPLAY': '$display', 'XDG_CURRENT_DESKTOP': 'GNOME'}" >/dev/null
# Give each new window focus once it is shown, as a desktop does when the user
# starts an app. Without an activation token, some GNOME versions (46) leave a
# test fixture unfocused, and keys are then correctly refused. The shell opens
# the overview at startup, which can come after the first test window on a slow
# machine; it keeps keyboard focus on its search entry and covers every window,
# so this session leaves the overview whenever it opens.
for _ in $(seq 50); do
  result=$(gdbus call --session --dest org.gnome.Shell --object-path /org/gnome/Shell --method org.gnome.Shell.Eval \
    "Main.overview.connect('shown', () => Main.overview.hide()); Main.overview.hide(); global.display.connect('window-created', (display, window) => window.connect('shown', w => { Main.overview.hide(); w.activate(global.get_current_time()); })); 'ok'" 2>/dev/null) && break
  sleep 0.2
done
case "${result:-}" in
  *true*) ;;
  *) echo "gnome-session.sh: could not install the focus handler: ${result:-no reply}" >&2 ;;
esac
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
# GNOME 46 adds the keyboard to the seat only once it has sent a key, and GTK
# activates windows only on keyboard focus. Shift alone types nothing.
call("keyboard.chord", modifiers=[], key=0xffe1)
open(sys.argv[1]).read()
' "$log.keyboard" | OPEN_COMPUTER_USE_WAYLAND_TEST=1 "$OPEN_COMPUTER_USE_WAYLAND_HELPER" >"$log.keyboard-replies" 2>&1 &
  keyboard=$!
  trap 'echo > "$log.keyboard"; cleanup' EXIT
  sleep 2
  # Report the warm-up result: without a seat keyboard GTK never activates a window.
  if ! grep -q '"granted":true' "$log.keyboard-replies" 2>/dev/null; then
    echo "gnome-session.sh: the warm-up keyboard was not granted: $(tr -d '\r' < "$log.keyboard-replies" 2>/dev/null | tail -c 400)" >&2
  fi
fi
backend=wayland
if [ -z "$x11" ]; then
  for _ in $(seq 100); do
    x_socket=$(ls /tmp/.X11-unix 2>/dev/null | grep -vxF "$x_before" | head -1 || true)
    [ -n "$x_socket" ] && break
    sleep 0.1
  done
  [ -n "$x_socket" ] || { echo "gnome-session.sh: XWayland did not start" >&2; exit 1; }
  DISPLAY=":${x_socket#X}"
  export DISPLAY
  backend=x11
fi
env WAYLAND_DISPLAY="$display" XDG_SESSION_TYPE=wayland GDK_BACKEND="$backend" \
  XDG_CURRENT_DESKTOP=GNOME OPEN_COMPUTER_USE_WAYLAND_TEST=1 OPEN_COMPUTER_USE_LINUX_GNOME_TEST=1 \
  OPEN_COMPUTER_USE_LINUX_XWAYLAND_TEST="$([ "$backend" = x11 ] && echo 1)" "$@"
