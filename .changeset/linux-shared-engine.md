---
"@cherrystudio/computer-use": patch
---

Share one native AT-SPI engine between the Linux SDK runtime and CLI/MCP. The Linux SDK now supports secondary actions, `setValue` with range checks and read-back, and `typeText` into the one focused editable field; scroll, drag, key presses and pointer clicks stay unsupported because they need global input.
