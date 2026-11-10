---
"@cherrystudio/computer-use": patch
---

Add a read-only getAppIcon API for host UI presentation, independent of tool results and app control sessions. macOS, Windows and Linux return bounded PNGs. Windows resolves executable and AppsFolder icons; Linux reads desktop-entry icons through GTK 3 and GdkPixbuf (including SVG and theme icons), with unavailable fallback when the libraries or icon are missing.
