# @cherrystudio/computer-use

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
