---
"@cherrystudio/computer-use": patch
---

On Linux Wayland desktops, clicks on elements of X11 apps running under XWayland now land where the element is: GNOME draws shadows inside such windows, which shifted the computed points. Under GNOME, the CLI no longer sends XTEST input to XWayland windows, which XWayland there drops until a portal dialog allows it; those windows now go through the Shell extension or the RemoteDesktop portal like other windows, and with the extension, text outside ASCII is refused for them before anything is sent.
