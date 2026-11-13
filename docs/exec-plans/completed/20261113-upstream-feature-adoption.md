# 上游能力接入

## 目标与授权

用户确认开始实施[能力接入评估](../../references/upstream-feature-adoption-20261113.md)。在保留 Cherry SDK、CDP、Linux Go/Wayland、身份和发布流程的前提下，接入查询、文本选择、按需观察、截图配置、后台窗口、可选虚拟屏、Locked Use 及相关正确性和效率修复。

实施基线：Cherry `2d9bd95`、此前修复 `cb192a1`、upstream `bef6c05`。不引入 JS REPL、DeepSeek Harness 或上游版本发布体系。系统组件的代码和打包属于实施范围；实际管理员安装、锁屏与多显示器验收单独记录，不从单测推断通过。

## 调用与归属

- `query({ appSessionId, text?, role?, exact?, limit?, maxNodes?, windowId? })` 返回当前会话的查询快照，匹配元素位于 `snapshot.tree.elements`；后续动作仍必须绑定 `appSessionId`、`snapshotId`、`elementId`。不向 SDK 暴露 CLI 的独立高位索引 registry。
- `selectText` 纳入 `act` / `perform` 的动作联合，支持文字、前后文和选择/光标模式，原生确认实际选区。
- `getAppState` 补 `includeAX` 与截图选项，至少选择一个通道；只截图清除旧元素引用。图片结果声明实际 MIME，平台能力分别报告。
- macOS 后台窗口路径复用引擎，默认不激活；CDP 继续负责已有页面通道，已发生或不确定的操作不换通道重发。
- 窗口停放由应用会话持有，显示器与 Locked Use 租约由控制任务持有。停止单应用只释放该应用资源，任务关闭释放全部所属资源，清理未确认不能返回成功。
- Locked Use 默认关闭，明确启用后才允许获取租约；普通观察、诊断或工具调用不安装系统组件。Cherry 使用独立组件身份、安装路径及签名要求。

## 实现与验证

1. 整合 Cherry 主线，固定原生上游来源。
2. 移植共享原生能力及对应回归，保留 Cherry 的结构化元素、输入执行/观察分离和 SDK 生命周期。
3. 更新协议 schema、生成代码、TS 客户端、Swift/Go 分发和平台适配。未实现的平台返回明确能力错误。
4. 完成虚拟屏归属/恢复、Locked Use 任务调用边界与签名打包；检查所有直接输入路径。
5. 运行 Swift、SDK、Go、PowerShell、打包/版本和文档检查；可用权限下运行自有窗口回归。单测、真实桌面、系统安装/签名和远端 CI 分别记录。

## 进度

- [x] 用户确认实施范围。
- [x] 保留上轮评估并整合最新 Cherry 主线（唯一文本冲突为发布记录，保留双方内容）。
- [x] 共享原生能力与最新正确性修复。
- [x] SDK 查询、选择和观察契约。
- [x] 三端按需观察与可见节点预算。
- [x] macOS 后台、虚拟屏与任务清理。
- [x] Locked Use 组件、SDK 调用边界与打包。
- [x] 测试、文档、Changeset、提交与剩余实机边界。

## 验收重点

- 查询预算不足与空结果有区别；应用重启、窗口变化和停止后的引用失效。
- 文本选择覆盖 Unicode、重复文本、文本改变、只读/密码字段和失败不重发。
- 未请求的采集通道确实未运行；图像尺寸与坐标一致；PNG/JPEG/WebP 与客户端声明一致。
- 多窗口、sheet、遮挡和跨 Space 保持目标身份；用户前台输入不误投。
- 虚拟屏正常退出、恢复失败、显示拓扑变化和 helper 异常不丢失恢复依据。
- Locked Use 的任务结束、取消、物理接管与断连等待动作排空、重锁及保护进程清理；系统安装/升级/卸载可单独验证。

## 决策记录

- 2026-11-13：沿用评估中的 SDK 资源归属设计，按文件三方整合上游引擎和 Cherry 结构化输出，避免整仓替换。每个实现切片保留可运行验证，系统级真实验收不隐式触发。


## 完成结果与验收记录

- 完成 macOS query/selectText、观察选项与图像格式、后台窗口/键盘、虚拟屏、Locked Use 及 Cherry SDK 适配；Windows/Linux 保留原有引擎，接入按需观察、树预算、密码与输入身份修复。
- 追加上游 PR #114 的 `3dedad7` 显示布局修复。停放前保留原位置，必须确认窗口到位；恢复失败不丢弃记录。SDK 窗口归属、默认键盘焦点及截图窗口 ID 单独校验。
- 真实 AppKit 回归发现旧 attribute bridge 的空 AXIdentifier 返回通用 failure；仅把这一可选属性当作缺失，已有 identifier 消失仍拒绝，名称/角色校验保留。
- 完整 Swift 验证包含 536 项（9 项需显式实机开关而跳过）；三个独立 AppKit 回归包含 SDK 查询、Unicode 选区替换及输入/写值确认。CLI/MCP headless smoke 与 cursor idle smoke 通过。
- SDK 52 项、原生私有会话协议 7 项、Windows Go、Linux/共享 Go race 测试、PowerShell 观察/元素身份/键盘/写值/几何回归通过；Windows x64/arm64 编译通过。
- ad-hoc Cherry app 构建通过；认证插件编译与离线 ABI 拒绝/取消测试通过，未求值系统授权规则。无可用 Developer ID 证书，未执行正式签名、公证、安装或锁屏循环。
- Changeset、架构/SDK/安全/发布说明与 history 已同步；保留任务外验收在[技术债追踪](../tech-debt-tracker.md)。未推送或启动远端 CI。

代码接入完成不等于完整产品实机验收。系统组件安装、完整锁屏、暗屏/物理接管、多物理显示器/跨 Space，以及 Windows/Linux GUI 和 SCK 实际采集仍单独验收。未导入 JS REPL、DeepSeek Harness 或上游发布流程。
