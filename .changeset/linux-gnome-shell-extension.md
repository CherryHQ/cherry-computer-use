---
"@cherrystudio/computer-use": patch
---

On GNOME Wayland, the Linux runtime can now capture windows and send pointer and keyboard input through the Cherry Computer Use GNOME Shell extension (GNOME 46–50). Install it with `open-computer-use install-gnome-extension` and log in again; `requestPermissions({ ids: ['gnomeShell'] })` then shows the Shell's dialog, and a top bar indicator stops desktop control at any time. The extension captures covered windows, clicks only when the target owns the point, sends keys only to the focus window, and types any Unicode into text-input clients. `getPermissionStatus` reports `gnomeShell` on GNOME. Without the extension, GNOME keeps the RemoteDesktop portal keyboard.
