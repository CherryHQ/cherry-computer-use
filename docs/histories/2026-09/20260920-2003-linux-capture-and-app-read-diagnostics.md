## [2026-09-20 20:03] | Task: Fix Linux capture and app-read failures found by controlling Firefox

### 🤖 Execution Context
* **Agent ID**: `Claude Code`
* **Base Model**: `deepseek-flash[1m]`
* **Runtime**: `Ubuntu 24.04 / aarch64 / X11 / Go 1.27.1 / Node 24.21.0`

### 📥 User Query
> 用 `cherry-computer-use` 测试控制 Firefox，并把测试中发现的 bug 修掉。

### 🛠 Changes Overview
**Scope:** `apps/OpenComputerUseLinux`

**Key Actions:**
- **[Capture pixel format]**: `captureLinuxWindow` rejected every window whose X11 depth was not 24, so the ARGB (depth 32) windows that Firefox and other compositing clients map always failed with `CAPTURE_FAILED: X11 pixel format is unsupported`. The decision moved into `decodablePixelFormat(order, depth, width, height, data)`, which now accepts depth 24 and depth 32 over one 32-bit BGRA pixel per point and keeps rejecting big-endian servers, non-32-bit depths and short replies.
- **[App read diagnostics]**: `Apps()` silently skipped any registered application whose accessibility name or process id could not be read, which is exactly what a sandbox policy denial looks like — Firefox simply vanished from `listApps()` with no error and no log. Skips now keep the underlying D-Bus error, land on stderr with the registered/skipped counts, and a desktop that registers applications but yields none returns `TARGET_UNAVAILABLE` instead of an empty list.
- **[Tests]**: `TestCaptureDecodesDepth32Windows` covers the depth-32 regression plus the rejected formats, and `TestUnreadableDesktopIsNotReportedAsEmpty` pins the error/list/empty split.

### 🧠 Design Intent (Why)
Both defects were found by driving the real SDK against Firefox rather than the repository fixture, and both were invisible to the existing suite. The fixture is a plain GTK window at depth 24, so the capture path never met an ARGB window; and the fixture's accessibility name is always readable, so the skip branch never ran.

The capture guard treated depth 24 as a proxy for "32 bits per pixel of RGB". That proxy holds for plain windows but not for ARGB ones, which are the same wire shape with an alpha byte the decoder already discards. Checking depth membership and the reply length states the actual requirement instead.

The skip branch was worse than a missing feature: the SDK design contract requires collection state to distinguish "nothing there" from "not allowed to read", and an empty `AppList` tells an agent the desktop is empty. `AppInfo.name` has `minLength: 1`, so an unreadable application cannot be represented in the list today; the smallest change that keeps that promise without a wire change is to make the condition visible — a stderr line carrying the real cause, and an error rather than an empty list when nothing at all could be read. Adding structured `unavailable` entries to `AppList` is the fuller fix and remains open.

A snap-confined Firefox on a desktop whose policy only admits `peer=(label=unconfined)` peers is the case that surfaced this. The policy lives in the snap package and the host application profile, so it is not fixable here; what this change buys is that the failure now names itself instead of looking like an absent application.

### 📁 Files Modified
- `apps/OpenComputerUseLinux/sdk_capture.go`
- `apps/OpenComputerUseLinux/sdk.go`
- `apps/OpenComputerUseLinux/sdk_test.go`

### ✅ Verification
- `go vet ./...`, `gofmt`, `go test ./...` in `apps/OpenComputerUseLinux`; `go test -race ./...` in `packages/runtime-go`.
- `npm run sdk:test` (36/36), `node --test protocol/native.test.mjs` (4 passed, 2 macOS-only skipped), and the GTK `protocol/desktop.test.mjs` contract test still passes with `COMPUTER_USE_REQUIRE_SCREENSHOT=1`, covering the depth-24 path.
- Real Firefox on X11: `listApps()` skips it with the AppArmor cause on stderr when the caller is confined, and from an unconfined caller the same window now returns a 2532x1372 PNG that renders Firefox correctly instead of `CAPTURE_FAILED`.
