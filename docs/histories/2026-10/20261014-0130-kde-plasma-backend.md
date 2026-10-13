# KDE Plasma backend

Request: design the KDE backend, verify it on Plasma, then open a PR and implement it.

- Design (W5 in the display-backends doc): official KWin facilities only, so no compositor plugin has to be rebuilt per Plasma release.
  - A KWin script reports windows, the active window, the cursor and `windowAt`.
  - `ScreenShot2.CaptureWindow` captures one window.
  - `org_kde_kwin_fake_input` moves, clicks and types.
  - Both restricted interfaces are authorized by a `.desktop` file naming the helper's executable.
- Feasibility in an isolated KWin 6.6.6:
  - Script queries take ~25 ms.
  - Capture works on a covered window, and authorization gating works.
  - Cursor position and `windowAt` are correct right after a move; clicks and keys work.
  - Plasma panels and notifications are reported as dock and notification windows.
  - Two outputs and 1.5x scaling work.
- Helper (`kwin.rs`):
  - Per-query scripts with a token; only reports from KWin's bus name are accepted.
  - ScreenShot2 capture through a pipe.
  - A persistent fake_input connection.
  - Clicks wait until the cursor is at the point and the target is topmost there; keys until the target is active. Text the layout lacks is refused before sending.
- Go: the GNOME extension and KWin share one compositor-assisted route (`wayland.Route`). The SDK reports `kdeIntegration`.
- CLI: `install-kde-integration` / `uninstall-kde-integration`. The install waits until KWin serves the helper.
- Tests:
  - `testdata/kwin-session.sh`, optionally with plasmashell, scaling and several outputs.
  - `TestKDEIntegration`, plus the SDK desktop test with and without the integration.
  - A KDE CI job on ubuntu-26.04.
- Found on the way:
  - The isolated KWin followed the desktop's logind lock state and started behind a fullscreen lock-screen fallback window while the desktop was locked. The session script now passes `--no-lockscreen`.
  - zbus runs on tokio in this build because ashpd enables the feature, so the report receiver needs a runtime.
  - KDE's application cache only lists `.desktop` files an application menu includes. With only KWin installed (the CI runner) there is no menu, so KWin never found the authorization. The session script now sets `XDG_MENU_PREFIX` as startplasma does, or writes a menu of every application into the private config when Plasma's menu is absent. The installer names the missing menu when it times out.
  - The ScreenShot2 probe counted any error other than NoAuthorized, such as a timeout, as authorized. Only KWin's InvalidWindow answer, given after its permission check, counts now.
  - ScreenShot2 comes from KWin's screenshot plugin, which is in kwin-common on Debian and Ubuntu. On Ubuntu 26.04, kwin-wayland does not pull kwin-common in, though a full Plasma install does. The CI job installs it. The helper reports a missing plugin, and the installer passes ScreenShot2's error on.
  - KWin captures windows only with OpenGL compositing; under QPainter every CaptureWindow is cancelled. The CI runner lacked Mesa's software driver and a DRM device, which KWin's virtual backend needs for OpenGL, so KWin fell back to QPainter. CI now installs Mesa and loads vgem, as KWin's own CI does, and the helper reports a cancelled capture under QPainter as needing OpenGL compositing.

Validation: isolated KWin 6.6.6 locally — the engine test and the SDK desktop test with the integration installed by the runtime itself; the SDK test without it; unit tests for installing and window geometry.

Files: `wayland-helper/src/kwin.rs`, `internal/display/wayland/targeted.go`, `internal/desktop/display.go`, `sdk.go`, `kde_integration.go`, `testdata/kwin-session.sh`, `internal/desktop/kde_test.go`, `protocol/linux-desktop.test.mjs`, `.github/workflows/sdk-check.yml`.
