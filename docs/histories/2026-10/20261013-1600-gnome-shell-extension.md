# GNOME Shell extension backend

Request: open a new PR and build the GNOME Shell extension (W4) designed in #10: window identity, per-window capture, and input from virtual devices inside the Shell, authorized by the user.

- `apps/OpenComputerUseLinux/gnome-shell-extension/computer-use@cherryai` (GNOME 46–50) serves `com.cherryai.ComputerUse.Shell1` on the session bus:
  - window list;
  - per-window PNG capture, including covered windows (a transparent clone keeps them painting);
  - activation and the window at a point;
  - clicks and drags that confirm the target owns the point in the same call;
  - key chords and text for the focus window, with text committed through the Shell input method so any Unicode reaches text-input clients.
- Authorization:
  - Nothing is served until the user allows the calling executable (`/proc/<pid>/exe` of the bus peer) in a Shell dialog.
  - The grant lasts until the screen locks or the session ends; a top bar indicator revokes it.
- The Wayland helper calls the extension through `shell.rs`. GNOME capture, location and input go through it, and extension errors keep their code and whether input may already have been sent.
- The engine uses the client surface (including client-side shadows) as the origin for element points. A missing grant is `PERMISSION_REQUIRED` for the SDK; the CLI shows the dialog once and retries. The SDK reports `gnomeShell`.
- `open-computer-use install-gnome-extension` / `uninstall-gnome-extension` write and enable the embedded extension, or remove it. Installing tells the user to log in again, and never changes a disabled user-extension setting.
- Tests: `gnome-session.sh` runs the runtime's own installer before the shell starts. `TestGNOMEShellExtension` and the SDK desktop test cover the grant flow, capture of a covered window, refused and accepted clicks, drag, keys, Unicode text, refusal while another window has focus, and revocation. CI runs GNOME 50 and 46, with and without the extension.

Validation: isolated headless GNOME 50 locally — the extension test 4/4 and the SDK test 3/3 with the extension; the portal keyboard and SDK tests without it; unit tests for installing and parsing. First probe: the real extension worked over D-Bus on first load.

Later fixes from CI and the real desktop:

- Click refusals name what covers the point.
- The isolated session turns notification banners off and leaves the startup overview.
- The extension waits for clients to see pointer motion, confirms the cursor actually reached the point before pressing (a new device's first motion was dropped on the GNOME 50 runner), and warms up a new virtual pointer.

Real desktop (GNOME 50.1 in a Parallels VM):

- Setup:
  - The user turned on user extensions, which were off; the installer only reports that.
  - Then logged in again.
  - Authorized in the Shell dialog.
- Results:
  - Capture, double click, coordinate click, Ctrl+A and BackSpace all worked.
  - After stopping from the top bar and allowing again, a fresh virtual pointer clicked on its first try while the mouse was idle.
  - Moving the mouse right after allowing makes the host's absolute pointer override the virtual one, and the click is rightly refused.

Files: `apps/OpenComputerUseLinux/gnome-shell-extension/`, `wayland-helper/src/shell.rs`, `internal/display/wayland/shell.go`, `internal/desktop/display.go`, `sdk.go`, `gnome_extension.go`, `testdata/gnome-session.sh`, `protocol/linux-desktop.test.mjs`, `.github/workflows/sdk-check.yml`.
