## [2026-10-14 05:30] | Task: Hyprland 与 XWayland 接入 CI，验证旧门户按键接口

### 🤖 Execution Context
* **Agent ID**: `Claude Code`
* **Base Model**: `claude-opus-5-5`
* **Runtime**: `Claude Code desktop app (Code tab), Linux arm64`

### 📥 User Query
> 先做 Hyprland/XWayland 进 CI，然后看看门户的旧式按键接口（RemoteDesktop 的 NotifyKeyboardKeysym 等 D-Bus 方法，早于 libei），需要先验证这些老门户是否可用。

### 🛠 Changes Overview
**Scope:** `apps/OpenComputerUseLinux`、`.github/workflows`、`docs`、`skills`

**Key Actions:**
- **[Hyprland CI]**: 新的 `ubuntu-26.04` 任务，Hyprland 嵌套在无头 labwc 中。嵌套的 Hyprland 只在父合成器的座位有输入设备时创建输出，会话脚本因此给 labwc 一个虚拟键盘和指针与一个无头输出；runner 用 vgem 加 Mesa 软件渲染，vgem 主节点挂载到渲染节点上（dumb buffer 只在主节点上可分配），`udevadm settle` 之后再放开权限。
- **[XWayland]**: 各会话脚本支持 `OCU_XWAYLAND=1`，Sway、Hyprland、GNOME、KWin 的任务都对 XWayland 窗口再运行一遍测试；新增 `TestXWaylandXTEST`。
- **[元素坐标]**: XWayland 窗口的元素按屏幕坐标放置，前提是可访问窗口与合成器的内容区对齐（GTK 在 Mutter 下把阴影画在 X 窗口内，窗口坐标不可用；KWin 的标题栏算在窗口内）。
- **[GNOME 下不走 XTEST]**: Mutter 以 `-enable-ei-portal` 启动 XWayland，XTEST 在门户对话框允许前被丢弃而引擎报告成功；GNOME 下的 XWayland 窗口改走扩展或门户键盘。扩展下布局外字符对 X11 窗口在发送前拒绝。
- **[测试脚本]**: GNOME 脚本把 `WAYLAND_DISPLAY` 交给总线后续启动的服务、从 Mutter 读取 `DISPLAY` 与 `XAUTHORITY`；KDE 任务补上 `udevadm settle`（此前偶发退回 QPainter）。
- **[旧门户]**: GNOME 42 的门户只有版本 1（无 `ConnectToEIS`，有 `NotifyKeyboard*`），Mutter 42 的旧方法能把按键送进 GTK 窗口，布局外 keysym 被静默丢弃；结论写入设计文档。

### 🧠 Design Intent (Why)
Hyprland 此前被认为需要 GPU 与带显示器的父合成器才能运行，实际缺的是父合成器座位上的输入设备；接入 CI 后 Omarchy 这类用户的路线有了持续验证。XWayland 一直标为待验证，实测发现了两个真实问题：GNOME 下 XWayland 窗口的元素坐标会错位，CLI 的 XTEST 输入会被静默丢弃。旧门户按键接口先验证再实现，为 Ubuntu 22.04、Debian 12、RHEL 9 恢复 Wayland 下的按键做准备。

### 📁 Files Modified
- `apps/OpenComputerUseLinux/internal/desktop/display.go`
- `apps/OpenComputerUseLinux/internal/desktop/xwayland_test.go`
- `apps/OpenComputerUseLinux/internal/desktop/gnome_shell_test.go`
- `apps/OpenComputerUseLinux/testdata/wlroots-session.sh`
- `apps/OpenComputerUseLinux/testdata/gnome-session.sh`
- `apps/OpenComputerUseLinux/testdata/kwin-session.sh`
- `.github/workflows/sdk-check.yml`
- `docs/design-docs/linux-display-backends.md`
- `skills/open-computer-use/references/troubleshooting.md`
