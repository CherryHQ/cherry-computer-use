---
"@cherrystudio/computer-use": patch
---

Share one native AT-SPI/X11 engine between the Linux SDK runtime and CLI/MCP, and drop the Linux runtime's Python dependency. The Linux SDK supports secondary actions, `setValue` with range checks and read-back, and `typeText` into the one focused editable field. On X11, pointer clicks, drag, key presses, page scrolling and typing without a focused field require `allowGlobalInput: true`, check that the target window has focus or lies under the pointer, and are released by a guard process if the runtime exits abnormally. Wayland reports them unsupported.
