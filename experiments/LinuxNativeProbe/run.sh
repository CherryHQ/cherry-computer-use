#!/usr/bin/env bash
set -euo pipefail

probe_arch="${PROBE_ARCH:-arm64}"
export COMPUTER_USE_RUNTIME_PATH="/workspace/dist/linux/${probe_arch}/open-computer-use"
node --test protocol/native.test.mjs
if command -v python3; then
  echo "The runtime probe image must not contain Python" >&2
  exit 1
fi
export DISPLAY=:99 GTK_A11Y=always NO_AT_BRIDGE=0
export XDG_RUNTIME_DIR=/tmp/cherry-probe-runtime
mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"
Xvfb :99 -screen 0 1024x768x24 >/tmp/xvfb.log 2>&1 &
xvfb_pid=$!
trap 'kill "$xvfb_pid" 2>/dev/null || true' EXIT
export PROBE_ARCH="$probe_arch"
for attempt in $(seq 1 100); do
  if [ -S /tmp/.X11-unix/X99 ]; then break; fi
  sleep 0.1
done
dbus-run-session -- bash -euo pipefail -c '
  gdbus call --session --dest org.a11y.Bus --object-path /org/a11y/bus --method org.freedesktop.DBus.Properties.Set org.a11y.Status IsEnabled "<true>"
  /usr/local/bin/cherry-native-fixture >/tmp/cherry-probe-window &
  fixture_pid=$!
  trap '\''kill "$fixture_pid" 2>/dev/null || true'\'' EXIT
  for attempt in $(seq 1 100); do
    if [ -s /tmp/cherry-probe-window ]; then break; fi
    sleep 0.1
  done
  COMPUTER_USE_FIXTURE_APP="cherry-native-fixture" COMPUTER_USE_REQUIRE_SCREENSHOT=1 node --test protocol/desktop.test.mjs
  kill "$fixture_pid"
  wait "$fixture_pid" 2>/dev/null || true
  /usr/local/bin/cherry-native-fixture >/tmp/cherry-probe-window &
  fixture_pid=$!
  sleep 0.5
  window_id=$(cat /tmp/cherry-probe-window)
  timeout 20 env PATH=/nonexistent "/workspace/dist/linux/${PROBE_ARCH}/native-probe" -window "$window_id" -screenshot /tmp/cherry-probe.png
'
