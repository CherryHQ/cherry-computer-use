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
