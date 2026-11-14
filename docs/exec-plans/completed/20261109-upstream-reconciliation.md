# 上游差异评估与必要修复移植

## 目标

核对 `iFurySt/open-computer-use` 的最新主线，解决适用于 Cherry 的修复在当前架构中的冲突，并明确哪些上游功能值得后续接入。

## 背景与范围

- Cherry 基线：`db6da8520793e210b9bd1b66e39b95a0dc02b5c0`。
- 上游基线：`1263960a888e77297e551b560206bd44fd1b53e5`。
- 共同祖先：`386a260d1ab8b690adbbb27f7471595cf0c2b752`。
- 上游独有 75 个提交，448 个文件变化；`git merge-tree --write-tree` 报告 30 个冲突。
- Cherry 已独立实现 SDK 协议 v2、应用会话、CDP 页面输入、Go AT-SPI/Wayland 引擎、Changesets 和六个平台包。不能直接替换为上游文件。
- 本轮先处理会导致误操作、崩溃或错误成功反馈的修复，以及边界清晰的请求资源和 CLI 路径修复。保留 Cherry 的 SDK、身份、版本与发布流程。
- Locked Use、虚拟显示器、JS REPL、query/select_text、图片格式配置属于新增产品能力，单独评估，不混入修复。
- 选择性移植不会把整个 upstream 标记为已合并；不使用 ours merge 隐藏未接入功能。

## 风险与验证

- macOS SDK 与 CLI/MCP 共享部分引擎，但键盘路径不同；分别检查调用方，保留 SDK 的 CDP 与失效目标检查。
- 上游 Python Linux 修复不能恢复已经删除的后端；先核对 Go 引擎是否已有等效行为。
- Windows GUI 验收与 macOS 单测是独立证据；不能把跨平台编译视为 Windows 桌面验收。
- 运行 Swift 测试、相关 Go/脚本测试、文档检查与差异检查；有条件时运行隔离 fixture 验证。

## 进度

- [x] 确认 fork、远端、共同祖先和冲突范围。
- [x] 核对必要性：Retina hit test 已修复；Linux Go 已有实时焦点和 set_value 读回；Windows SDK 已按目标窗口截图。
- [x] 逐项移植本轮适用修复并处理语义差异；来源及后续项见 [评估](../../references/upstream-reconciliation-20261109.md)。
- [x] 补齐回归覆盖、架构说明、发布说明与 [history](../../histories/2026-11/20261109-0542-upstream-reconciliation.md)。
- [x] Swift、AppKit 真实输入回归、SDK、Go、PowerShell 函数测试与文档检查通过；未完成录屏和 Windows 真实桌面验收。

## 决策

- 2026-11-09：先采用选择性修复范围；完整同步会引入锁屏进程、私有显示能力和独立发布体系，需要单独接入设计。
- 2026-11-13：完成用户点名能力的[后续评估](../../references/upstream-feature-adoption-20261113.md)，建议分批适配全部这些能力。复核 upstream `bef6c05` 和 Cherry `2d9bd95`，确认 SDK 已有部分观察/权限接口，并列出虚拟屏及 Locked Use 的具体恢复和验证缺口。本计划完成状态仍只代表第一批修复与后续评估，不代表新增功能已交付；实现时应建立相应执行计划。
