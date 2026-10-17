# @cherrystudio/computer-use

## 0.1.8

### Patch Changes

- [#26](https://github.com/CherryHQ/cherry-computer-use/pull/26) [`528567b`](https://github.com/CherryHQ/cherry-computer-use/commit/528567bc50e77bdfc5fcf5d2ac57babd4a3b6cf6) Thanks [@DeJeune](https://github.com/DeJeune)! - Support identity-authorized installed application launch on Windows and Linux.
  Windows resolves AppsFolder applications and canonical executables; Linux resolves
  XDG desktop entries and launches them through D-Bus activation or Exec, without GIO tools. Preserve running-only rebinding,
  revalidate identity before effects, and wait for the matching accessible window.

## 0.1.7

### Patch Changes

- [#24](https://github.com/CherryHQ/cherry-computer-use/pull/24) [`144f513`](https://github.com/CherryHQ/cherry-computer-use/commit/144f5138bac1188813191e2aa2bb5e75365ca4c6) Thanks [@DeJeune](https://github.com/DeJeune)! - Derive `listApps()` app IDs from the running process, so an ID stays valid across runtime restarts until the app exits.

## 0.1.6

### Patch Changes

- [#22](https://github.com/CherryHQ/cherry-computer-use/pull/22) [`eccd8bd`](https://github.com/CherryHQ/cherry-computer-use/commit/eccd8bd4579339a6e5f37b53d19998a3231af0ef) Thanks [@DeJeune](https://github.com/DeJeune)! - Resolve installed macOS application identities without launching so hosts can enforce stopped-app policy before effects. Return stable identities on running apps and validate the authorized identity before launch.

## 0.1.5

### Patch Changes

- [#20](https://github.com/CherryHQ/cherry-computer-use/pull/20) [`4b491ca`](https://github.com/CherryHQ/cherry-computer-use/commit/4b491ca57b494cea33e9d2aceda5b616a2079a4a) Thanks [@DeJeune](https://github.com/DeJeune)! - Add `perform` for actions without implicit capture, preserving snapshot-scoped element input and revalidated keyboard targets. Add `resolveApp` with unambiguous running-app lookup and background application launch on macOS. Existing `act` behavior remains compatible. Install matching SDK/native packages to use the new methods.

  Add `includeScreenshot: false` for AX-only observations without native capture on all three platforms.

## 0.1.4

### Patch Changes

- [#17](https://github.com/CherryHQ/cherry-computer-use/pull/17) [`3f56efb`](https://github.com/CherryHQ/cherry-computer-use/commit/3f56efb1a2ca6be266dd3b5f821a1bfd7ba3f808) Thanks [@DeJeune](https://github.com/DeJeune)! - On Linux Wayland desktops, clicks on elements of X11 apps running under XWayland now land where the element is: GNOME draws shadows inside such windows, which shifted the computed points. Under GNOME, the CLI no longer sends XTEST input to XWayland windows, which XWayland there drops until a portal dialog allows it; those windows now go through the Shell extension or the RemoteDesktop portal like other windows, and with the extension, text outside ASCII is refused for them before anything is sent.

## 0.1.3

### Patch Changes

- [#12](https://github.com/CherryHQ/cherry-computer-use/pull/12) [`c749f60`](https://github.com/CherryHQ/cherry-computer-use/commit/c749f60239b20823dabc34795dde9fde3f9e6d1a) Thanks [@DeJeune](https://github.com/DeJeune)! - On GNOME Wayland, the Linux runtime can now capture windows and send pointer and keyboard input through the Cherry Computer Use GNOME Shell extension (GNOME 46–50). Install it with `open-computer-use install-gnome-extension` and log in again; `requestPermissions({ ids: ['gnomeShell'] })` then shows the Shell's dialog, and a top bar indicator stops desktop control at any time. The extension captures covered windows, clicks only when the target owns the point, sends keys only to the focus window, and types any Unicode into text-input clients. `getPermissionStatus` reports `gnomeShell` on GNOME. Without the extension, GNOME keeps the RemoteDesktop portal keyboard.

- [#14](https://github.com/CherryHQ/cherry-computer-use/pull/14) [`2cee038`](https://github.com/CherryHQ/cherry-computer-use/commit/2cee0387d445f4b9055978d69a05dffc3ea5f7de) Thanks [@DeJeune](https://github.com/DeJeune)! - On KDE Plasma 6 (Wayland), the Linux runtime can now capture windows and send pointer and keyboard input through KWin. Run `open-computer-use install-kde-integration` once (no new login): it authorizes the runtime's helper for KWin's window capture and input. Clicks are pressed only once the cursor is at the point and the target is the topmost window there, so panels, notifications and other windows are detected; keys go only to the active target, and text the keyboard layout lacks is refused unless it goes into a focused text field. `getPermissionStatus` reports `kdeIntegration`.

## 0.1.2

### Patch Changes

- [#10](https://github.com/CherryHQ/cherry-computer-use/pull/10) [`3be2d65`](https://github.com/CherryHQ/cherry-computer-use/commit/3be2d6568817f5b3b154b1fab45ed6340be28e52) Thanks [@DeJeune](https://github.com/DeJeune)! - Share one native AT-SPI/X11 engine between the Linux SDK runtime and CLI/MCP, and drop the Linux runtime's Python dependency. The Linux SDK supports secondary actions, `setValue` with range checks and read-back, and `typeText` into the one focused editable field. On X11, pointer clicks, drag, key presses, page scrolling and typing without a focused field require `allowGlobalInput: true`, check that the target window has focus or lies under the pointer, and are released by a guard process if the runtime exits abnormally. On Wayland, key presses, page scrolling and typing without a focused field use a virtual keyboard on Sway and Hyprland (no dialog, any character, bound to the compositor's focused window) and the RemoteDesktop portal elsewhere after `requestPermissions({ ids: ['remoteDesktop'] })`. On Sway and Hyprland, window screenshots come from the compositor (Sway screen copy of the window's region, Hyprland toplevel export) at the window's logical size, and pointer clicks and drag use a virtual pointer that must leave the target focused under the pointer (focus follows the mouse) before any button is pressed; other Wayland compositors report capture and pointer input as unsupported. The Linux runtime packages now ship the Wayland helper (`open-computer-use-wayland`) next to the runtime, linked against glibc 2.28 so it starts on Debian 10, RHEL 8 and later.

- [#8](https://github.com/CherryHQ/cherry-computer-use/pull/8) [`67ee9e9`](https://github.com/CherryHQ/cherry-computer-use/commit/67ee9e94631be0fcb8eae7e09e59a217a3e8c36f) Thanks [@DeJeune](https://github.com/DeJeune)! - Use the existing Windows engine for all SDK actions and structured observation. Fix UTF-8 command decoding on Chinese Windows and return malformed-payload errors without disabling the session. Preserve snapshot validation, cancellation and directed input cleanup.

## 0.1.1

### Patch Changes

- [#5](https://github.com/CherryHQ/cherry-computer-use/pull/5) [`b6a68c2`](https://github.com/CherryHQ/cherry-computer-use/commit/b6a68c2bbe0946ca0b0b792288cc74f9de34cb0d) Thanks [@DeJeune](https://github.com/DeJeune)! - Install the matching native runtime automatically on macOS, Windows and Linux (arm64/x64). macOS release helpers are Developer ID signed and notarized. Validate installed SDK and runtime tarballs before publishing; keep the CLI private.

## 0.1.0

### Patch Changes

- [#2](https://github.com/CherryHQ/cherry-computer-use/pull/2) [`f854441`](https://github.com/CherryHQ/cherry-computer-use/commit/f854441522345489fb0b5857883228c93016917c) Thanks [@DeJeune](https://github.com/DeJeune)! - Adopt Cherry Computer Use branding and the dedicated macOS helper identity. Publish the TypeScript SDK under the Cherry Studio scope; native CLI distribution remains local only.
