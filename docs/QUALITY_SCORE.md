# 质量评分

## 评分标准

- `A`：覆盖完整、行为稳定、文档清楚、运行风险低。
- `B`：整体可接受，但还有明确短板。
- `C`：能用，但需要针对性补强。
- `D`：脆弱、缺少规范，或很多行为尚未定义。

## 当前水位

| 区域 | 评分 | 原因 | 下一步 |
| --- | --- | --- | --- |
| TypeScript SDK | C | 类型化客户端、协议校验、ESM/CJS tarball 与三端生命周期通过本机验证；Windows 11 ARM64/Linux X11 的真实 SDK 观察、截图和语义点击通过。 | macOS 签名 SDK 已验证真实 AppKit 查询/选区/输入与 PNG/JPEG/WebP 截图，跨 Space 仍待验收；macOS 权限申请与 Cherry 本地入口已接通；新增六架构 SDK 平台包安装/启动验收；Windows SDK 已适配原有七种动作；Linux SDK 已接入语义动作与 X11 全局输入（需显式允许），Wayland 与 Electron 完整桌面验收仍需独立确认。 |
| 产品面 | B | 已经有 Swift 本地 `computer-use` MCP server、默认 app 模式权限引导，以及原有 9 个 tools；macOS 新增 query/select_text，三端支持按需观察；真实窗口已验证后台焦点隔离、保存弹窗、SDK 停止恢复及拥有者 SIGKILL 后的虚拟屏恢复。 | 仍需多物理显示器/跨 Space、签名安装和完整锁屏循环验收。 |
| Windows runtime | C | 已新增独立 Go `.exe`，通过 Windows UI Automation + Win32 window message 暴露同样 9 个 tools、MCP server 和 `call --calls`；默认不再自动启动 app、执行 `SetFocus`，或让 `type_text` 走可能抢前台的 UIA text fallback，并已接入 npm bundled artifact 分发；已补齐物理 DPI、滚轮坐标/方向与经典子控件点击的生产函数回归。 | 补混合 DPI/多屏与真实子控件验收、交互式桌面 smoke、installer/signing，以及更原生的 Go UIA 实现或更稳定的 bridge。 |
| Linux runtime | C | 已新增独立 Go binary，暴露同样 9 个 tools、MCP server 和 `call --calls`；观察、语义动作与 X11 全局输入已改为 SDK 共用的 Go 引擎，运行时不再依赖 Python；全局输入在隔离 X11 会话中验收并接入 CI；Ubuntu GNOME VM 已跑通 `list_apps`、MCP tools list 和 Text Editor 8-tool sequence，并已接入 npm bundled artifact 分发，但截图在 GNOME Wayland 下仍只能 best-effort，coordinate input 也不是通用后台模型。 | x64 与发行包实际启动验收、Wayland portal 截图与输入、非 GTK 应用验证。 |
| 架构文档 | B | 顶层结构、fixture bridge、app 模式、Cherry 发布身份与签名 / 公证流程已落文档。 | 补正式 CI 发布验收和 host 打包集成。 |
| 测试 | B | `swift test` + smoke suite 覆盖原有工具及文本选择；新增查询、编码、预算、任务归属与 Locked Use 状态机回归，并新增了针对“前台焦点是否被抢占”的手工对比样本沉淀；上游移植补充了可重复的独立 AppKit 点击、实时焦点、选区与写值读回回归，以及 Windows PowerShell 函数测试。 | 增加更多普通 app 的录制回归，减少只依赖 fixture 和一次性手工检查。 |
| 可观测性 | C | 已有 `doctor`、`snapshot`、smoke 输出，以及一组仓库内留档的官方 `computer-use` / 本仓库实现对比样本。 | 补统一日志级别、失败上下文和 release artifact 里的诊断信息，把一次性样本收敛成可重复采集流程。 |
| 安全 | B | 已明确本地-only、权限边界和 fixture bridge 作用域；新增密码字段保护、窗口/元素身份校验及默认关闭的 Locked Use 租约；HID 启动期输入和权限撤销拒绝继续，部分设备被独占时的监控探针已运行。 | 完成 Locked Use 签名组件安装/升级/卸载、Karabiner/Secure Input 下物理接管和完整锁屏实机验证；宿主 session approval 仍独立演进。 |

新增能力的平台差异与尚未完成的实机边界见[接入说明](design-docs/upstream-desktop-capabilities.md)。评分未因单测增加而上调。
