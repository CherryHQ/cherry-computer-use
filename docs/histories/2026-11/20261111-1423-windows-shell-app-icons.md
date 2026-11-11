## [2026-11-11 14:23] | Task: Extend Windows Shell app icon coverage

### Execution Context

- Agent: Codex
- Runtime: Windows 11 x64

### User Query

Fill the Windows icon gaps in PR #35 for AppsFolder entries and TextInputHost, then
rerun the complete icon coverage test.

### Changes

- The read-only `resolveAppIdentity` request can resolve an exact AppsFolder entry
  to a `win32:shell:` identity when its target has no launchable identity. The
  Windows icon reader checks the entry against AppsFolder before asking the Shell
  for its image. Launch resolution continues to use executable and packaged app
  identities.
- Packaged identities can be read directly through the Shell, including
  TextInputHost, whose AUMID is absent from the Start menu enumeration.
- Added native regression coverage for the Control Panel entry, TextInputHost
  when discoverable, and missing identities. Updated the SDK, protocol and release
  documentation.

### Validation

- The built SDK and Windows runtime returned icons for all 184 Start menu
  entries and all 11 running apps on Windows 11 x64, up from 144 and 10.
  All 195 images decoded as 64×64 PNGs with visible pixels. App control
  session counts were zero before and after the scan.
- Windows runtime and shared Go tests passed. The opt-in Windows cold-launch
  test passed. The symlink test requires a privilege unavailable on the test host.
- SDK type checks passed. The SDK suite reported 46 passed and two platform skips.
- The Windows ARM64 build and documentation check passed.
