#!/bin/sh
# Run a command in an isolated, headless wlroots-family compositor with its own
# D-Bus session, AT-SPI bus and XDG directories. Nothing is shown on, or sent to,
# the desktop in use.
#
#   testdata/wlroots-session.sh sway <command...>
#   testdata/wlroots-session.sh hyprland <command...>
#
# sway runs headless with software rendering. Hyprland has no headless mode, so it
# runs nested inside a headless labwc, which needs a GPU render node for dmabuf.
# Where a headless parent gives it no monitor, OCU_HYPRLAND_PARENT=wayland-0 nests
# it in that display instead: a window appears there, but input still goes only to
# the nested Hyprland's own seat.
# The command gets WAYLAND_DISPLAY, XDG_SESSION_TYPE=wayland, SWAYSOCK or
# HYPRLAND_INSTANCE_SIGNATURE, and OPEN_COMPUTER_USE_LINUX_WLROOTS_TEST=<compositor>.
set -eu
compositor=${1:-}
case "$compositor" in
  sway | hyprland) shift ;;
  *) echo "usage: $0 sway|hyprland <command...>" >&2; exit 2 ;;
esac
[ $# -gt 0 ] || { echo "usage: $0 $compositor <command...>" >&2; exit 2; }

if [ "${OCU_WLROOTS_SESSION_INNER:-}" != 1 ]; then
  # The private directories must be set before dbus-run-session starts: services
  # it activates inherit its environment, and sharing the desktop's directories
  # lets them replace its sockets (the accessibility bus) or write its state.
  # A short root: Hyprland's IPC socket lives several levels below it and Unix
  # socket paths are limited to 108 bytes.
  root=$(mktemp -d "${TMPDIR:-/tmp}/ocu-wl.XXXXXX")
  for dir in run config data cache state; do mkdir -m 700 "$root/$dir"; done
  if [ -n "${OCU_HYPRLAND_PARENT:-}" ]; then
    case "$OCU_HYPRLAND_PARENT" in
      /*) ;;
      *) OCU_HYPRLAND_PARENT="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/$OCU_HYPRLAND_PARENT" ;;
    esac
    [ -S "$OCU_HYPRLAND_PARENT" ] || { echo "wlroots-session.sh: no Wayland display at $OCU_HYPRLAND_PARENT" >&2; exit 1; }
    export OCU_HYPRLAND_PARENT
  fi
  if command -v go >/dev/null 2>&1; then
    GOCACHE=$(go env GOCACHE)
    export GOCACHE
  fi
  exec env -u DBUS_SESSION_BUS_ADDRESS -u DISPLAY -u WAYLAND_DISPLAY -u AT_SPI_BUS_ADDRESS -u XAUTHORITY \
    -u SWAYSOCK -u HYPRLAND_INSTANCE_SIGNATURE -u I3SOCK \
    XDG_RUNTIME_DIR="$root/run" XDG_CONFIG_HOME="$root/config" XDG_DATA_HOME="$root/data" \
    XDG_CACHE_HOME="$root/cache" XDG_STATE_HOME="$root/state" \
    OCU_WLROOTS_SESSION_INNER=1 OCU_WLROOTS_SESSION_ROOT="$root" dbus-run-session -- "$0" "$compositor" "$@"
fi
root=${OCU_WLROOTS_SESSION_ROOT:-}
case "$root" in
  "${TMPDIR:-/tmp}"/ocu-wl.*) ;;
  *) echo "wlroots-session.sh: missing private session root" >&2; exit 1 ;;
esac

pids=""
touch "$root/started"
# session_processes lists every process carrying this session's root in its
# environment: compositors that re-exec away from their parent and services the
# private bus activated. It skips this script and dbus-run-session above it.
session_processes() {
  for dir in /proc/[0-9]*; do
    pid=${dir#/proc/}
    [ "$pid" = $$ ] || [ "$pid" = "$PPID" ] && continue
    [ -O "$dir" ] || continue
    { tr '\0' '\n' < "$dir/environ"; } 2>/dev/null | grep -qxF "OCU_WLROOTS_SESSION_ROOT=$root" && echo "$pid"
  done
}
# Cleanup must never change the command's exit status.
cleanup() {
  set +e
  [ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ] && hyprctl dispatch exit >/dev/null 2>&1
  for pid in $pids; do kill "$pid" 2>/dev/null; done
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    left=$(session_processes)
    [ -z "$left" ] && break
    kill $left 2>/dev/null
    sleep 0.3
  done
  left=$(session_processes)
  [ -n "$left" ] && kill -9 $left 2>/dev/null
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    mounts=$(awk -v root="$root/" 'index($2, root) == 1 { print $2 }' /proc/mounts)
    [ -z "$mounts" ] && break
    for mount in $mounts; do fusermount3 -u -z "$mount" 2>/dev/null || fusermount -u -z "$mount" 2>/dev/null; done
    sleep 0.5
  done
  rm -rf "$root" 2>/dev/null
  (for _ in 1 2 3 4 5; do sleep 1; rm -rf "$root" 2>/dev/null; done) &
}
trap cleanup EXIT

# wait_for_socket <pid> <count>: until the runtime directory holds count Wayland
# sockets, or the compositor exits. Prints the newest socket name.
wait_for_socket() {
  for _ in $(seq 100); do
    kill -0 "$1" 2>/dev/null || { echo "wlroots-session.sh: compositor exited" >&2; return 1; }
    sockets=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -type s -name 'wayland-*' -newer "$root/started" | sort)
    if [ "$(printf '%s\n' "$sockets" | grep -c .)" -ge "$2" ]; then
      basename "$(ls -t $sockets | head -1)"
      return 0
    fi
    sleep 0.1
  done
  echo "wlroots-session.sh: no Wayland socket appeared" >&2
  return 1
}

case "$compositor" in
  sway)
    # Our own config only: the system config would export this session's
    # environment into the user's systemd manager.
    printf 'output HEADLESS-1 resolution 1024x768\nxwayland disable\n' > "$root/sway.conf"
    WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 XDG_CURRENT_DESKTOP=sway \
      sway -c "$root/sway.conf" > "$root/compositor.log" 2>&1 &
    pids="$!"
    WAYLAND_DISPLAY=$(wait_for_socket "$!" 1)
    SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -type s -name 'sway-ipc.*' | head -1)
    export SWAYSOCK
    ;;
  hyprland)
    if [ -z "$(find /dev/dri -name 'renderD*' 2>/dev/null)" ]; then
      echo "wlroots-session.sh: nested Hyprland needs a GPU render node in /dev/dri" >&2
      exit 1
    fi
    sockets=1
    if [ -n "${OCU_HYPRLAND_PARENT:-}" ]; then
      WAYLAND_DISPLAY=$OCU_HYPRLAND_PARENT
    else
      # An empty config directory: no system or user autostart.
      mkdir -m 700 "$root/labwc"
      WLR_BACKENDS=headless WLR_RENDERER=gles2 WLR_LIBINPUT_NO_DEVICES=1 \
        labwc -C "$root/labwc" > "$root/parent.log" 2>&1 &
      pids="$!"
      WAYLAND_DISPLAY=$(wait_for_socket "$!" 1)
      sockets=2
    fi
    export WAYLAND_DISPLAY
    mkdir -p "$XDG_CONFIG_HOME/hypr"
    cat > "$XDG_CONFIG_HOME/hypr/hyprland.conf" <<'CONF'
monitor = , 1024x768, auto, 1
xwayland {
  enabled = false
}
# Window animations advance per rendered frame; a nested headless output may
# barely render, leaving new windows at their tiny opening size.
animations {
  enabled = false
}
misc {
  disable_hyprland_logo = true
  disable_splash_rendering = true
}
ecosystem {
  no_update_news = true
  no_donation_nag = true
}
CONF
    HYPRLAND_NO_CRASHREPORTER=1 XDG_CURRENT_DESKTOP=Hyprland \
      Hyprland --config "$XDG_CONFIG_HOME/hypr/hyprland.conf" > "$root/compositor.log" 2>&1 &
    pids="$! $pids"
    WAYLAND_DISPLAY=$(wait_for_socket "$!" "$sockets")
    for _ in $(seq 50); do
      HYPRLAND_INSTANCE_SIGNATURE=$(ls "$XDG_RUNTIME_DIR/hypr" 2>/dev/null | head -1)
      [ -n "$HYPRLAND_INSTANCE_SIGNATURE" ] && break
      sleep 0.1
    done
    export HYPRLAND_INSTANCE_SIGNATURE
    ;;
esac
export WAYLAND_DISPLAY

launcher=$(command -v at-spi-bus-launcher || echo /usr/libexec/at-spi-bus-launcher)
"$launcher" --launch-immediately >/dev/null 2>&1 &
pids="$pids $!"
sleep 1
# A headless compositor has no keyboard or pointer, and clients bind one only
# after the seat gains it, losing input sent meanwhile. Hold a virtual keyboard
# and pointer for the whole run, as physical devices do (needs
# OPEN_COMPUTER_USE_WAYLAND_HELPER).
if [ -n "${OPEN_COMPUTER_USE_WAYLAND_HELPER:-}" ]; then
  mkfifo "$root/keyboard"
  python3 -c '
import json, sys
def call(method, **params):
    body = json.dumps({"id": 1, "method": method, "params": params}).encode()
    sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    sys.stdout.buffer.flush()
call("hello", protocol=1)
call("vkeyboard.prepare")
call("vpointer.prepare")
open(sys.argv[1]).read()
' "$root/keyboard" | "$OPEN_COMPUTER_USE_WAYLAND_HELPER" >/dev/null &
  pids="$pids $!"
  sleep 1
fi
status=0
env XDG_SESSION_TYPE=wayland GDK_BACKEND=wayland OPEN_COMPUTER_USE_LINUX_WLROOTS_TEST="$compositor" "$@" || status=$?
exit "$status"
