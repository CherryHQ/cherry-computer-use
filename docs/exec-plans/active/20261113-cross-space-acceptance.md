# 跨 Space 实机验收

## 目标与范围

在同一物理显示器的两个真实 Desktop 之间验证后台窗口的控件树、点击、文字与快捷键输入，以及有录屏授权时的真实截图。使用临时 Chrome profile 和本地测试页面，不操作用户窗口；测试结束和失败均恢复原 Space、前台应用与遮挡维护状态。显式开启测试后，缺少第二个 Desktop 时通过 Dock 辅助功能创建临时桌面，并在确认清空后删除。

本轮不将单显示器结果扩展为多物理屏幕、热插拔、完整锁屏或跨平台验收。新增验收仍保留 Cherry SDK、输入通道和任务归属契约。

## 前置条件与风险

- 辅助功能权限与可用的调度中心；截图另需当前测试身份的录屏权限。自动准备桌面会短暂显示调度中心。
- 仅在 setup/cleanup 切换 Desktop，实际观察与输入期间断言当前 Space、前台进程和目标窗口归属不变。
- 单独报告未满足条件的测试，不把 skip 计作通过。
- 测试宿主及签名 app 的新 LaunchServices 进程均具备两项权限。临时 Desktop 自动创建、切换与删除已实测；用户反馈调度中心影响鼠标操作后，停止会切换桌面的测试。已确认无验收进程残留、两块屏幕均恢复到原来的单个 Desktop。

## 验证方式

`OPEN_COMPUTER_USE_RUN_CROSS_SPACE_LIVE_TEST=1 OPEN_COMPUTER_USE_VISUAL_CURSOR=0 swift test --filter CrossSpaceLiveTests`

同时核对测试进程、窗口 Space 归属、实际 AX 读回/页面状态、图像解码及清理后的 Space/进程状态。按变更运行完整 Swift 回归与文档检查。

签名 app 的独立验收入口：`OPEN_COMPUTER_USE_RUN_SDK_DESKTOP_LIVE_TEST=1 OPEN_COMPUTER_USE_VISUAL_CURSOR=0 node --test protocol/macos-desktop-live.test.mjs`。默认使用 release bundle，不禁用 app agent，也不以宿主继承权限代替签名应用授权。

## 进度

- [x] 检查现有验收代码、机器显示器/Space 与权限。
- [x] 拆分控件树/输入与截图验收，补齐前置条件和异常清理；新增 SDK query/selectText/typeText 与 stop 检查。
- [ ] 运行可执行的实机场景，记录结果或具体阻塞条件。
- [x] 更新 history、验收边界，并准备 PR 说明。

## 决策

- 2026-11-13：CGWindow 的标题受录屏权限限制，AX/输入测试应通过所属进程的 AX 窗口读取标题并绑定 Window ID，避免把标题不可读误报成窗口不存在。
- 2026-11-13：原测试只在成功路径释放遮挡维护，且在切换 Space 后才记录前台应用；先修复恢复逻辑再执行。
- 2026-11-13：测试编译通过。显式开启测试后，两项分别因录屏未授权和当前显示器只有一个 Desktop 而跳过，尚无跨 Space 操作通过的证据。系统设置显示已有 Cherry 授权条目，但当前签名制品仍报告未授权，须以新进程的运行时结果复核，不能仅凭设置开关判断。
- 2026-11-13：重新添加当前签名制品的两项权限并重启后，新 LaunchServices app agent 与 SDK 私有 agent 均确认授权。禁用 CLI proxy 得到的授权属于宿主继承路径，不能用作这一结论的证据。
- 2026-11-13：公开 SDK 的真实 AppKit 验收通过：PNG/JPEG/WebP 均为 640×463，解码和非空像素检查通过；query/selectText/skyKey 后 fixture 读回 `hello 跨Space🙂`，停止/关闭完成且目标应用仍存活。测试保存到 `protocol/macos-desktop-live.test.mjs`。
- 2026-11-13：Chrome 后台键盘首次运行观察到鼠标移动和前台窗口失去 key；提示保持桌面静止后，相同用例通过，鼠标坐标完全一致，前台 activation/key-window loss 计数不变。前一次运行不计为通过。
- 2026-11-13：用户打开笔记本屏幕后，在线枚举变为两块物理屏幕，各一个普通 Desktop；这不满足同显示器两个 Space 的测试条件。跨 Space 两项仍跳过，计划保持进行中。
- 2026-11-13：进一步核对自动化方案：本机存在 `SLSSpaceCreate` / `CGSSpaceCreate`，但符号存在不能证明可直接创建调度中心管理的普通桌面。参考 [Hammerspoon 的 Spaces 实现](https://github.com/Hammerspoon/hammerspoon/blob/master/extensions/spaces/spaces.lua)，直接读取 Dock AX 树成功，`CoreDockSendNotification` 能打开/关闭调度中心，两块屏幕均暴露 `mc.spaces.add`。新增测试专用准备器，通过 AX 创建、切换和删除桌面；不修改 SIP，也不向 Dock 注入组件。读回实际 Space ID、窗口归属和离屏状态验证结果。
- 2026-11-13：实测自动创建与删除普通 Desktop 成功。Chrome 目标确实位于另一个 Space 且离屏，AX、定向点击、输入和快捷键读回成功。发现 Chromium 接受选区写入后延迟约 100 毫秒才发布新范围，修复为一次写入后有时限等待读回，期间继续校验文字和任务有效性；12 项选区测试通过，包含延迟发布、等待时文字变化和撤销。
- 2026-11-13：修复后跨 Space 的 SDK query/selectText/typeText 读回成功，但该轮一项在调度中心准备阶段未找到桌面列表，另一项观察到前台应用和鼠标位置变化，共两项测试、三处失败，不能记作完整验收通过。用户反馈鼠标受到干扰，交互式复测停止；自动创建/切换仅属于显式测试准备，不加入普通 SDK 任务启动流程。跨 Space 功能用于访问已有其他桌面的窗口，无需为“不抢鼠标”额外创建桌面。
- 2026-11-13：停止桌面操作后，调度中心准备器改为等待目标显示器的完整控件列表；编译及 11 项非交互选区回归通过，两项跨 Space 测试按开关跳过，准备器的最新等待逻辑未实机复测。当前代码的 arm64 release app 和四组件重新完成 Developer ID 签名、架构/身份检查及离线认证/内核签名验证；未安装系统组件。
