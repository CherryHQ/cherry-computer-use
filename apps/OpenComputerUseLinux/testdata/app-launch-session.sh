#!/bin/sh
# Run inside an isolated desktop session. The SDK, rather than this script, launches GTK:
# once through Exec and once through D-Bus activation, as GNOME applications start.
set -eu
root=$(pwd)
fixture="$root/apps/OpenComputerUseLinux/testdata/gtk_fixture.py"
mkdir -p "$XDG_DATA_HOME/applications" "$XDG_DATA_HOME/dbus-1/services"
launcher="$XDG_DATA_HOME/applications/org.cherry.SDKLaunch.desktop"
cat > "$launcher" <<EOF
[Desktop Entry]
Type=Application
Name=Cherry SDK Cold Launch
Exec=/usr/bin/python3 "$fixture" "SDK Cold Launch" %U
Terminal=false
EOF
id=org.cherry.SDKDBusLaunch
command="/usr/bin/python3 \"$fixture\" --application-id $id \"SDK D-Bus Launch\""
cat > "$XDG_DATA_HOME/dbus-1/services/$id.service" <<EOF
[D-BUS Service]
Name=$id
Exec=$command --gapplication-service
EOF
cat > "$XDG_DATA_HOME/applications/$id.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Cherry SDK D-Bus Launch
DBusActivatable=true
Exec=$command --new-window
EOF
# Desktop sessions hand their display to D-Bus activation once the compositor runs;
# isolated sessions start the bus first, so activated services would lack it.
for variable in WAYLAND_DISPLAY DISPLAY XDG_SESSION_TYPE XDG_CURRENT_DESKTOP GDK_BACKEND; do
  eval "[ -n \"\${$variable:-}\" ]" && dbus-update-activation-environment "$variable"
done
export COMPUTER_USE_RUNTIME_PATH="$root/dist/native/open-computer-use"
COMPUTER_USE_LAUNCH_APP="$launcher" node --test protocol/app-launch.test.mjs
COMPUTER_USE_LAUNCH_APP="$id" node --test protocol/app-launch.test.mjs
