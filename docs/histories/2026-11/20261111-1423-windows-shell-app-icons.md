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

### Follow-up: Shell parsing and Linux running applications

- Guard filesystem-only alias and extension extraction against invalid path
  characters in Shell virtual targets, so an unrelated AppsFolder entry cannot
  abort Control Panel identity resolution. Added an isolated virtual-target
  regression alongside the existing native Control Panel icon test.
- Running Linux apps that cannot match a desktop entry use a canonical executable
  presentation identity. Read EWMH window icons on X11/Xwayland and fall back to
  the lowercase executable's installed theme icon without launching the app.
- Added malformed ARGB, alpha preservation, icon-size selection, missing-process,
  canonical-identity and executable theme-icon coverage.
- Linux desktop retest: 7 of 8 running apps have identities (previously 4), and
  5 return icons (previously 4), including Codex. The remaining VMware process
  has no readable executable; GNOME Shell and gjs expose no matching icon.
  Installed desktop entries remain at 83 of 97 icons, with zero app sessions.
- PowerShell 7 on Linux verified isolated Shell resolution and launch rejection;
  Windows PowerShell/.NET Framework and native Control Panel acceptance require
  the Windows CI run. Linux PowerShell does not reproduce Framework path errors.
