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
- Linux resolves XDG desktop entries and uses GIO to launch them. Do not parse
  Exec into a shell command. Running processes carry the same installed identity.
- Verify the expected identity before launch, wait for the matching accessible
  window, and report possible effects after dispatch without replaying launch.
- Do not promise background launch on Windows/Linux: desktop activation policy
  can foreground an application. No global keyboard/input workaround is used.

## Verification

1. Shared contract: installed lookup has no effect; mismatches, ambiguity,
   cancellation and running-only rebinding never launch; delayed windows and
   post-launch failures preserve the effect boundary.
2. Platform tests: Windows native executable and packaged-app identity/launch;
   Linux desktop discovery, overrides, localization, process association and GIO.
3. Build both platform runtimes and run available real desktop fixtures. Record
   unavailable host/session coverage rather than inferring GUI success.
4. Update SDK/host guidance and add a release changeset; keep published dependency
   pins unchanged until matching native artifacts are published.

## Progress

- [x] Confirmed Windows and Linux share the unsupported launch implementation.
- [x] Implement shared identity and launch flow.
- [x] Implement Windows and Linux platform drivers.
- [x] Validate shared contracts, Windows executable and Calculator cold launch.
- [ ] Linux real-machine acceptance (user will run it from the draft PR).
- [x] Synchronize documentation, release notes and history.

## Draft handoff

Windows x64 executable/Calculator cold launch and x64/arm64 builds passed. Linux
desktop discovery tests and x64/arm64 builds passed. A WSL GTK attempt reached
the app but lacked GIO's desktop-file path metadata; launch now supplies that
standard variable explicitly, and terminal-only entries cannot claim unrelated
GUI processes through executable fallback. Linux GUI acceptance remains pending.
The user requested a draft PR before further local Linux investigation.
