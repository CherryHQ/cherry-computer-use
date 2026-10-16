#!/bin/sh
# Run inside an isolated desktop session. The SDK, rather than this script, launches GTK.
set -eu
root=$(pwd)
mkdir -p "$XDG_DATA_HOME/applications"
launcher="$XDG_DATA_HOME/applications/org.cherry.SDKLaunch.desktop"
cat > "$launcher" <<EOF
[Desktop Entry]
Type=Application
Name=Cherry SDK Cold Launch
Exec=/usr/bin/python3 "$root/apps/OpenComputerUseLinux/testdata/gtk_fixture.py" "SDK Cold Launch"
Terminal=false
EOF
export COMPUTER_USE_RUNTIME_PATH="$root/dist/native/open-computer-use"
export COMPUTER_USE_LAUNCH_APP="$launcher"
node --test protocol/app-launch.test.mjs
