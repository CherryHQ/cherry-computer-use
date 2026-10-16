## 2026-10-16 | Task: Add Windows and Linux SDK application launch

### Execution context

- Agent: Codex (GPT-6), local Windows workspace with WSL Linux build checks.

### User request

Complete installed application launch on Windows and check Linux as well; submit
a draft PR so the user can perform Linux acceptance on their own machine.

### Changes

- Add optional installed-identity/launch drivers to the shared Go SDK runtime.
- Carry stable installed identity through app discovery, sessions and snapshots;
  preserve expected-identity checks and running-only reacquisition.
- Resolve Windows AppsFolder/AUMID and executable aliases; launch through native
  Windows APIs outside the UIA worker's Job Object.
- Resolve Linux XDG desktop entries and launch with GIO, preserving desktop-file
  metadata for process association; avoid interpreting Exec as a shell command.
- Wait for a matching accessible window, retain possible effects after dispatch,
  and cover identity mismatch, ambiguity, cancellation and permission revocation.
- Add cross-platform cold-launch acceptance coverage, CI hooks, SDK documentation
  and a Changesets patch release entry.

### Validation and remaining acceptance

- Shared Go resolver/desktop/app-stop regression tests passed; Go vet passed.
- Windows executable identity, packaged Calculator identity and cold executable
  launch passed. Calculator cold launch passed through the TypeScript SDK.
- Windows executable symlink test skipped because the host lacks symlink privilege.
- Linux desktop-entry tests and SDK consumer tests passed; Linux x64/arm64 and
  Windows x64/arm64 runtime builds passed.
- TypeScript SDK build/typecheck and five affected SDK client tests passed.
- Cherry host control/REPL tests: 31 passed; scoped lint/format and docs gate passed.
- Linux GUI acceptance is pending. WSL GTK exposed missing GIO desktop-file path
  metadata and an unsafe terminal-executable association; both were addressed
  before draft submission, with real-machine retest left to the user.
- SDK/native packages have not been published; the Changeset prepares that follow-up.

### Key files

- `packages/runtime-go/app_resolver.go`
- `apps/OpenComputerUseWindows/sdk_apps.ps1`
- `apps/OpenComputerUseWindows/sdk_launch_windows.go`
- `apps/OpenComputerUseLinux/internal/desktopentry/desktopentry.go`
- `apps/OpenComputerUseLinux/sdk_launch.go`
- `protocol/app-launch.test.mjs`

## 2026-10-16 | Follow-up: Linux real-machine acceptance and native launch

### Execution context

- Agent: Claude Code, Ubuntu 24.04 GNOME Wayland (arm64) with Xephyr isolation.

### User request

Test the PR's behavior; then handle launch across Linux desktops without GIO.

### Changes

- Real-machine acceptance found that `gio launch` never delivers `Activate` to
  `DBusActivatable` entries (GNOME Clocks, Text Editor, Files, Settings and others),
  which time out with no window.
- Launch now calls `org.freedesktop.Application.Activate` over the session bus,
  falling back to Exec only when no service is installed; Exec is expanded per the
  specification (quoting, field codes, `Path`) and started detached with GIO launch
  metadata. Terminal entries are rejected. CI no longer installs GIO tools.
- App discovery maps owners of activatable application bus names to identities,
  because D-Bus-activated service command lines differ from Exec.
- The GTK fixture can run as a D-Bus-activatable `Gtk.Application`; Go and SDK
  cold-launch tests cover both Exec and D-Bus activation.

### Validation

- Real desktop: Calculator (Exec), Clocks, Text Editor and Files (D-Bus) cold-launch
  through the SDK with correct identity and accessible trees.
- Isolated X11 session: Go cold-launch test passes for both paths; with the old
  `gio launch` dispatch the D-Bus case times out. SDK protocol test passes for both.
- Linux Go unit tests and vet pass, including Exec expansion and bus-name rules.
- The SDK launch test (Exec and D-Bus) passes in isolated X11, KWin, Sway,
  Hyprland and headless GNOME Shell, each with native Wayland and XWayland apps
  where applicable, and in KWin with GIO tools hidden. CI now runs it in each
  compositor job. Isolated sessions start D-Bus before the compositor, so the test
  session passes the display to D-Bus activation as desktop sessions do.

### Key files

- `apps/OpenComputerUseLinux/sdk_launch.go`
- `apps/OpenComputerUseLinux/internal/desktopentry/desktopentry.go`
- `apps/OpenComputerUseLinux/testdata/app-launch-session.sh`
- `apps/OpenComputerUseLinux/testdata/gtk_fixture.py`
