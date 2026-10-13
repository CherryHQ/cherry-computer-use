---
"@cherrystudio/computer-use": patch
---

On KDE Plasma 6 (Wayland), the Linux runtime can now capture windows and send pointer and keyboard input through KWin. Run `open-computer-use install-kde-integration` once (no new login): it authorizes the runtime's helper for KWin's window capture and input. Clicks are pressed only once the cursor is at the point and the target is the topmost window there, so panels, notifications and other windows are detected; keys go only to the active target, and text the keyboard layout lacks is refused unless it goes into a focused text field. `getPermissionStatus` reports `kdeIntegration`.
