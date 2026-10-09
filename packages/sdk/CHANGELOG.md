# @cherrystudio/computer-use

## 0.1.2

### Patch Changes

- [#8](https://github.com/CherryHQ/cherry-computer-use/pull/8) [`67ee9e9`](https://github.com/CherryHQ/cherry-computer-use/commit/67ee9e94631be0fcb8eae7e09e59a217a3e8c36f) Thanks [@DeJeune](https://github.com/DeJeune)! - Use the existing Windows engine for all SDK actions and structured observation. Fix UTF-8 command decoding on Chinese Windows and return malformed-payload errors without disabling the session. Preserve snapshot validation, cancellation and directed input cleanup.

## 0.1.1

### Patch Changes

- [#5](https://github.com/CherryHQ/cherry-computer-use/pull/5) [`b6a68c2`](https://github.com/CherryHQ/cherry-computer-use/commit/b6a68c2bbe0946ca0b0b792288cc74f9de34cb0d) Thanks [@DeJeune](https://github.com/DeJeune)! - Install the matching native runtime automatically on macOS, Windows and Linux (arm64/x64). macOS release helpers are Developer ID signed and notarized. Validate installed SDK and runtime tarballs before publishing; keep the CLI private.

## 0.1.0

### Patch Changes

- [#2](https://github.com/CherryHQ/cherry-computer-use/pull/2) [`f854441`](https://github.com/CherryHQ/cherry-computer-use/commit/f854441522345489fb0b5857883228c93016917c) Thanks [@DeJeune](https://github.com/DeJeune)! - Adopt Cherry Computer Use branding and the dedicated macOS helper identity. Publish the TypeScript SDK under the Cherry Studio scope; native CLI distribution remains local only.
