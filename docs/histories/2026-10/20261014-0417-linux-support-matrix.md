## [2026-10-14 04:17] | Task: 记录 Linux 支持矩阵

### 🤖 Execution Context
* **Agent ID**: `Claude Code`
* **Base Model**: `claude-opus-5-5`
* **Runtime**: `Claude Code desktop app (Code tab), Linux arm64`

### 📥 User Query
> 在文档中记录 Linux 发行版（多数归为一类，只单列特殊的）、协议、合成器、桌面与支持功能构成的矩阵。

### 🛠 Changes Overview
**Scope:** `docs/design-docs`

**Key Actions:**
- **[合成器接口]**: 列出 Mutter、KWin、Sway、Hyprland 提供的窗口列表、截图、输入与门户接口；Mutter、KWin、Sway 的 Wayland 全局对象由辅助进程在隔离会话中实测列出。
- **[路线功能]**: X11、XWayland、GNOME（有无扩展）、KDE（Plasma 6 有无集成、Plasma 5）、Sway、Hyprland、其他 wlroots 与其他合成器的定位、截图、遮挡截图、指针、按键、布局外字符、一次性设置与验证状态。
- **[发行版]**: 跟进上游的发行版按桌面归为一类，单列 Ubuntu 26.04/24.04/22.04、Kubuntu、Debian 12/13、RHEL 9/10、Linux Mint、Pop!_OS、不可变发行版、NixOS 与 glibc 早于 2.28 的情况。

### 🧠 Design Intent (Why)
用户问的是"为什么不用协议实现"。矩阵把答案落在文档里：标准协议缺少 PID、全局坐标、落点窗口和按窗口截图，GNOME、KDE 也几乎不实现相关协议；同时让用户按发行版查到自己能用什么、缺了该怎么办（例如登录 Xorg 会话）。推断的条目用 ☑️ 与验证过的条目区分。

### 📁 Files Modified
- `docs/design-docs/linux-display-backends.md`
