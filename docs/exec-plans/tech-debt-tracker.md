# 技术债追踪

这里记录那些暂时不阻塞当前任务、但已经值得留档的技术债。

| 日期 | 区域 | 债务描述 | 为什么会存在 | 计划中的后续动作 |
| --- | --- | --- | --- | --- |
| 2026-04-17 | 普通 app AX snapshot | Finder 路径已经能拿到前台窗口子树并输出 window-relative frame，但当前还缺更多真实 app 回归样本，无法证明这套 rooting / traversal 对复杂 app 都稳定。 | 这一轮先把 Finder 这类真实 app 的坐标换算和窗口子树收敛好，再把 deterministic 回归继续留给 fixture。 | 增加 Safari / System Settings / Activity Monitor 等真实 app 样本验证，并继续收敛 `kAXMainWindowAttribute`、focused element parent chain 和多窗口回退策略。 |
| 2026-10-16 | Linux 应用启动 | 启动的应用没有自己的 systemd scope，会留在宿主进程的 scope 里；也没有传 Wayland activation token / 启动通知。 | 首版启动只覆盖 Desktop Entry 规范的 D-Bus 激活和 Exec，先保证跨桌面可用。 | 每次启动用 `systemd-run --user --scope` 建 `app-<id>-<随机>.scope`（无 systemd 时回退 setsid）；评估向合成器申请 activation token。 |
| 2026-10-16 | Linux 应用可访问性 | 已在运行的 Qt / Firefox / Electron 应用，以及 D-Bus 激活的应用，拿不到按进程开启的无障碍开关；Snap / Flatpak 里的 Electron 检测不到，不加 `--force-renderer-accessibility`；Snap 应用只对 AppArmor 允许的对端（如 unconfined 宿主）应答 AT-SPI。 | 只对 SDK 启动的进程设置开关，避免修改 GNOME 会持久化的会话级无障碍设置。 | 设计一个经用户同意的会话级无障碍权限（`org.a11y.Status`）；为 Snap / Flatpak 增加 Chromium 检测；记录宿主的 AppArmor 要求。 |
| 2026-10-16 | Linux 应用启动验收 | 未验证：完整登录的 Plasma / Hyprland / Sway / KDE X11、XFCE / Cinnamon / MATE、真机 GNOME X11、x64 真机、非 systemd 发行版、重新登录后真机上的 Flatpak D-Bus 应用、Java（jEdit）和 Wine（安装时网络失败）。MuseScore 的 Qt Quick 界面大部分子节点为空。 | 测试机为 arm64 GNOME Wayland，缺少这些桌面与 sudo，网络下载失败。 | 在对应环境补跑 `apps/OpenComputerUseLinux/testdata/app-launch-session.sh` 与真实应用冷启动；安装 `default-jre libatk-wrapper-java-jni jedit wine` 后验证 Java / Wine。 |
