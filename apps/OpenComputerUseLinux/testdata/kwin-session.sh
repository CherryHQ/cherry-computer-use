#!/bin/sh
# Run a command in an isolated KWin (KDE Plasma) Wayland session with its own
# D-Bus session, AT-SPI bus and XDG directories. KWin renders to a virtual
# framebuffer; nothing is shown on, or sent to, the desktop in use.
#
#   testdata/kwin-session.sh <command...>
#
# The command gets WAYLAND_DISPLAY, XDG_SESSION_TYPE=wayland,
# XDG_CURRENT_DESKTOP=KDE and OPEN_COMPUTER_USE_LINUX_KDE_TEST=1. KWin reads
# application .desktop files from the private XDG_DATA_HOME, so authorizations a
# test writes there stay inside the session.
#
# OCU_KWIN_PLASMASHELL=1 also starts plasmashell (panels, notifications);
# OCU_KWIN_SCALE and OCU_KWIN_OUTPUTS set the virtual outputs' scale and count.
# OCU_KDE_INTEGRATION=<open-computer-use executable> runs its
# install-kde-integration once KWin is up, as a user would.
set -eu
[ $# -gt 0 ] || { echo "usage: $0 <command...>" >&2; exit 2; }

if [ "${OCU_KWIN_SESSION_INNER:-}" != 1 ]; then
  # The private directories must be set before dbus-run-session starts: services
  # it activates inherit its environment.
  root=$(mktemp -d "${TMPDIR:-/tmp}/ocu-kwin.XXXXXX")
  for dir in run config data cache state; do mkdir -m 700 "$root/$dir"; done
  if command -v go >/dev/null 2>&1; then
    GOCACHE=$(go env GOCACHE)
    export GOCACHE
  fi
  exec env -u DBUS_SESSION_BUS_ADDRESS -u DISPLAY -u WAYLAND_DISPLAY -u AT_SPI_BUS_ADDRESS -u XAUTHORITY \
    -u SWAYSOCK -u HYPRLAND_INSTANCE_SIGNATURE -u KDE_FULL_SESSION -u KDE_SESSION_VERSION \
    XDG_RUNTIME_DIR="$root/run" XDG_CONFIG_HOME="$root/config" XDG_DATA_HOME="$root/data" \
    XDG_CACHE_HOME="$root/cache" XDG_STATE_HOME="$root/state" \
    OCU_KWIN_SESSION_INNER=1 OCU_KWIN_SESSION_ROOT="$root" dbus-run-session -- "$0" "$@"
fi
root=${OCU_KWIN_SESSION_ROOT:-}
case "$root" in
  "${TMPDIR:-/tmp}"/ocu-kwin.*) ;;
  *) echo "kwin-session.sh: missing private session root" >&2; exit 1 ;;
esac

# session_processes lists every process carrying this session's root in its
# environment, including services the private bus activated. It skips this
# script and dbus-run-session above it.
session_processes() {
  for dir in /proc/[0-9]*; do
    pid=${dir#/proc/}
    [ "$pid" = $$ ] || [ "$pid" = "$PPID" ] && continue
    [ -O "$dir" ] || continue
    { tr '\0' '\n' < "$dir/environ"; } 2>/dev/null | grep -qxF "OCU_KWIN_SESSION_ROOT=$root" && echo "$pid"
  done
}
# Cleanup must never change the command's exit status.
cleanup() {
  set +e
  kill "$kwin" 2>/dev/null
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
kwin=""
trap cleanup EXIT

display="wayland-ocu-kwin"
# KWin ships no default config here; keep screen edges and effects that could
# cover windows out of the way. KWin follows the lock state of the logind session
# it runs in, which is the desktop's: --no-lockscreen keeps a locked desktop from
# covering this session with KWin's lock screen.
mkdir -p "$XDG_CONFIG_HOME"
printf '[Plugins]\nscreenedgeEnabled=false\n' > "$XDG_CONFIG_HOME/kwinrc"
XDG_CURRENT_DESKTOP=KDE QT_QPA_PLATFORM=offscreen \
  kwin_wayland --virtual --no-lockscreen --width 1024 --height 768 --scale "${OCU_KWIN_SCALE:-1}" --output-count "${OCU_KWIN_OUTPUTS:-1}" \
  --socket "$display" > "$root/kwin.log" 2>&1 &
kwin=$!
for _ in $(seq 100); do
  [ -S "$XDG_RUNTIME_DIR/$display" ] && break
  kill -0 "$kwin" 2>/dev/null || { echo "kwin-session.sh: kwin_wayland exited:" >&2; tail -20 "$root/kwin.log" >&2; exit 1; }
  sleep 0.1
done
for _ in $(seq 100); do
  dbus-send --session --print-reply --dest=org.kde.KWin /KWin org.freedesktop.DBus.Peer.Ping >/dev/null 2>&1 && break
  sleep 0.1
done
if [ "${OCU_KWIN_PLASMASHELL:-}" = 1 ]; then
  # plasmashell refuses to load its shell without the activity manager.
  for daemon in /usr/lib/*/libexec/kactivitymanagerd /usr/libexec/kactivitymanagerd; do
    [ -x "$daemon" ] && { WAYLAND_DISPLAY="$display" QT_QPA_PLATFORM=wayland "$daemon" > "$root/kactivitymanagerd.log" 2>&1 & break; }
  done
  WAYLAND_DISPLAY="$display" XDG_SESSION_TYPE=wayland XDG_CURRENT_DESKTOP=KDE QT_QPA_PLATFORM=wayland \
    plasmashell --no-respawn > "$root/plasmashell.log" 2>&1 &
  for _ in $(seq 100); do
    dbus-send --session --print-reply --dest=org.kde.plasmashell /PlasmaShell org.freedesktop.DBus.Peer.Ping >/dev/null 2>&1 && break
    sleep 0.1
  done
  sleep 2
fi
if [ -n "${OCU_KDE_INTEGRATION:-}" ]; then
  WAYLAND_DISPLAY="$display" XDG_SESSION_TYPE=wayland XDG_CURRENT_DESKTOP=KDE "$OCU_KDE_INTEGRATION" install-kde-integration || {
    echo "kwin-session.sh: install-kde-integration failed; KWin log:" >&2
    tail -40 "$root/kwin.log" >&2
    exit 1
  }
fi
launcher=$(command -v at-spi-bus-launcher || echo /usr/libexec/at-spi-bus-launcher)
"$launcher" --launch-immediately >/dev/null 2>&1 &
sleep 1
status=0
env WAYLAND_DISPLAY="$display" XDG_SESSION_TYPE=wayland XDG_CURRENT_DESKTOP=KDE GDK_BACKEND=wayland QT_QPA_PLATFORM=wayland \
  OPEN_COMPUTER_USE_LINUX_KDE_TEST=1 "$@" || status=$?
exit "$status"
