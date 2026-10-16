# Windows and Linux SDK application launch

## Goal and scope

Support explicit SDK binding to installed applications on Windows and Linux,
using the existing identity-before-effect contract. Keep observation/rebinding
running-only and preserve host user-stop and ownership decisions. Legacy MCP
launch policy is outside this change.

## Design

- Add an optional installed-application driver to the shared Go runtime.
- Windows resolves AppsFolder entries and executable paths; packaged apps use
  AUMIDs and desktop apps use canonical executable identities.
- Linux resolves XDG desktop entries and launches them per the Desktop Entry
  Specification, with no GIO tools: `DBusActivatable` entries get
  `org.freedesktop.Application.Activate` (Exec only when no service exists);
  others run their expanded Exec, never through a shell, detached from the
  runtime. Running processes carry the same installed identity, first through the
  owner of an activatable application's bus name.
- Verify the expected identity before launch, wait for the matching accessible
  window, and report possible effects after dispatch without replaying launch.
- Do not promise background launch on Windows/Linux: desktop activation policy
  can foreground an application. No global keyboard/input workaround is used.

## Verification

1. Shared contract: installed lookup has no effect; mismatches, ambiguity,
   cancellation and running-only rebinding never launch; delayed windows and
   post-launch failures preserve the effect boundary.
2. Platform tests: Windows native executable and packaged-app identity/launch;
   Linux desktop discovery, overrides, localization, process association, Exec
   expansion, and cold launch through both Exec and D-Bus activation.
3. Build both platform runtimes and run available real desktop fixtures. Record
   unavailable host/session coverage rather than inferring GUI success.
4. Update SDK/host guidance and add a release changeset; keep published dependency
   pins unchanged until matching native artifacts are published.

## Progress

- [x] Confirmed Windows and Linux share the unsupported launch implementation.
- [x] Implement shared identity and launch flow.
- [x] Implement Windows and Linux platform drivers.
- [x] Validate shared contracts, Windows executable and Calculator cold launch.
- [x] Linux real-machine acceptance on GNOME Wayland (Ubuntu 24.04, arm64).
- [x] Replace `gio launch`, which loses D-Bus activations, with native launch.
- [x] Synchronize documentation, release notes and history.

## Draft handoff

Windows x64 executable/Calculator cold launch and x64/arm64 builds passed. Linux
desktop discovery tests and x64/arm64 builds passed. A WSL GTK attempt reached
the app but lacked GIO's desktop-file path metadata; launch now supplies that
standard variable explicitly, and terminal-only entries cannot claim unrelated
GUI processes through executable fallback. Linux GUI acceptance remains pending.
The user requested a draft PR before further local Linux investigation.

## Linux acceptance follow-up

On GNOME Wayland, `gio launch` started a `DBusActivatable` application's service
but exited before its `Activate` call was delivered, so no window opened and the
service quit from inactivity (Clocks, Text Editor; 17 local entries are
activatable). GLib works around the same dbus-daemon race only in `gio open` and
`gtk-launch`. The runtime now activates over D-Bus itself and expands Exec in Go,
which also removes the GIO tool dependency that Debian-family KDE or minimal
compositor installations may lack. D-Bus-activated processes run a service
command line without launch metadata, so discovery first maps owned application
bus names to their entries. Terminal entries are rejected rather than launched.
Future work: a transient systemd scope per launch for desktops that track apps by
cgroup, and Wayland activation tokens if focus policy requires them.
