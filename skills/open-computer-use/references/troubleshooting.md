# Cherry Computer Use Troubleshooting

Read this reference when setup, permission checks, app discovery, snapshots, or actions fail.

## Unsupported macOS Version

The macOS runtime requires macOS 14.0 or later. Check the host version before troubleshooting permissions:

```sh
sw_vers -productVersion
```

On macOS versions earlier than 14.0, the binary cannot launch and may report a `dyld` or minimum-version incompatibility. `open-computer-use doctor`, Accessibility authorization, and Screen Recording authorization cannot resolve this error. Upgrade macOS or run Cherry Computer Use on a supported macOS, Windows, or Linux desktop.

## First Checks

Start with:

```sh
open-computer-use -h
open-computer-use doctor
open-computer-use call list_apps
```

On macOS 14.0 or later, `doctor` reports Accessibility and Screen Recording status. If either is missing, ask the user to approve the onboarding UI.

## App Not Found

If `get_app_state` cannot find an app:

1. Run `open-computer-use call list_apps`.
2. Use the app name or bundle identifier from that result.
3. Confirm the app is running and has a visible, non-minimized window.
4. On macOS, rerun `open-computer-use doctor`.

Do not silently switch to a different app when the requested target is not available.

## Empty Or Missing Snapshot

Common causes:

- The app has no visible window.
- The window is minimized, hidden, or on an unavailable desktop.
- macOS Screen Recording permission is missing.
- Windows or Linux commands are running outside the logged-in desktop session.
- Linux screenshot support is blocked by the compositor or desktop portal state.

Ask the user to bring the target app/window into a visible state when automation cannot do so safely.

## Truncated Text

Snapshot text is limited to 500 characters by default. If a visible chat message, email body, document paragraph, or form value ends with `...`, do not assume the page itself is missing content.

Request a larger or unlimited text limit explicitly:

```sh
open-computer-use call get_app_state --args '{"app":"TextEdit","text_limit":1000}'
open-computer-use call get_app_state --args '{"app":"TextEdit","text_limit":"max"}'
open-computer-use snapshot --text-limit 1000 TextEdit
open-computer-use snapshot --text-limit max TextEdit
```

`text_limit: "max"` only disables the text character limit. It does not remove the default 1200 node count limit, 64 level tree depth limit, screenshot size, permission, or desktop-session protections.

## Incomplete Long Pages Or Lists

If the screenshot clearly shows more visible content than the accessibility tree returns, the snapshot may have hit its default 1200 node or 64 level tree budget. Do not treat that as proof that the page failed to load.

Request a larger tree budget explicitly:

```sh
open-computer-use call get_app_state --args '{"app":"Google Chrome","max_tree_nodes":3000,"max_tree_depth":96}'
open-computer-use snapshot --max-tree-nodes 3000 --max-tree-depth 96 "Google Chrome"
```

Increasing the tree budget does not change text truncation, screenshot limits, permissions, or desktop-session requirements.

## Element Action Fails

If an element-targeted action fails:

1. Re-run `get_app_state`.
2. Confirm the `element_index` still exists and refers to the intended UI element.
3. Prefer `set_value` for settable text/value controls.
4. Prefer `perform_secondary_action` only for actions exposed in the state result.
5. Use coordinate `click`, `scroll`, or `drag` only after the semantic route is unavailable.

## Desktop Session Issues

Windows UI Automation and Linux AT-SPI require a live user desktop. SSH sessions, CI jobs, launch daemons, or services often do not have access to the GUI session even when the CLI binary starts successfully.

If desktop access is missing, ask the user to run the command from the logged-in desktop session or start the target app visibly in that session.

## Linux Accessible Interface Errors

If a Linux release reports that `Accessible` has no `is_text` or `is_editable_text` attribute, upgrade. Current releases need no Python: they read the tree and run actions natively over D-Bus and X11.

Coordinate clicks, `drag`, `press_key`, `scroll`, and `type_text` without a focused editable field send X11 input. Keys are refused unless the target window has keyboard focus, and clicks are refused when another window covers the point; bring the target forward first. Under Wayland, the CLI sends X11 input to XWayland windows, except under GNOME, where XWayland holds such input until a portal dialog allows it; there XWayland windows go through the Shell extension or the portal keyboard like other windows, and with the extension, text outside ASCII is refused for them because X11 clients take no input method text. On Sway and Hyprland, native Wayland windows get keys through a virtual keyboard without any dialog, and only while the compositor reports the target as the focused window. On GNOME, install the Cherry Computer Use Shell extension with `open-computer-use install-gnome-extension` and log out and back in; the first action that needs your windows then asks you to allow desktop control in a GNOME dialog, and a top bar indicator stops it at any time. With it, screenshots work (also for covered windows), clicks are refused when another window or Shell element is at the point, and keys and text are refused unless the target window has focus. On KDE Plasma 6, run `open-computer-use install-kde-integration` once (no new login); screenshots then work, clicks are refused when another window, a panel or a notification is at the point, keys are refused unless the target window is active, and characters the keyboard layout cannot type are refused unless a text field has focus. Rerun it after moving or upgrading Cherry Computer Use, since the authorization names the helper's path. If it reports that KDE's application menu is missing, install plasma-workspace (Plasma's own menu); without a menu KDE's application cache lists no applications and KWin cannot find the authorization. If it reports that KWin offers no ScreenShot2, install KWin's plugins (kwin-common on Debian and Ubuntu). Without these integrations, on GNOME and KDE, `press_key`, `scroll` and `type_text` without a focused field ask for remote input in a system dialog the first time (it may open behind other windows; look in the overview); keys are refused unless the target window is active, and characters the keyboard layout cannot type (for example Chinese) are refused. On Sway and Hyprland, coordinate clicks and `drag` move the shared pointer onto the target window and press only if the compositor then reports that window as focused; with focus-follows-mouse disabled, or another window covering the point, they are refused. Elsewhere (GNOME without the extension, KDE without the integration), coordinate clicks, `drag` and screenshots are not available for native Wayland windows. If GNOME reports user extensions are turned off, enable them in the Extensions app or ask your administrator.

`type_text` writes into the one focused editable field of the target window. If two fields report focus it refuses; focus the intended field first.

## Permission And Safety Issues

- Do not bypass macOS TCC prompts.
- Do not enable global pointer fallbacks unless the user asks for low-level diagnostic behavior.
- Do not interact with password managers or sensitive apps unless the user explicitly requests it.
- Pause before submitting, sending, deleting, purchasing, or approving anything externally visible.
