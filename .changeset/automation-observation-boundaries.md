---
"@cherrystudio/computer-use": patch
---

Add `perform` for actions without implicit capture, preserving snapshot-scoped element input and revalidated keyboard targets. Add `resolveApp` with unambiguous running-app lookup and background application launch on macOS. Existing `act` behavior remains compatible. Install matching SDK/native packages to use the new methods.

Add `includeScreenshot: false` for AX-only observations without native capture on all three platforms.
