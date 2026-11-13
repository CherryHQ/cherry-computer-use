## [2026-11-09 05:42] | Task: 评估上游分歧并移植必要修复

### 🤖 Execution Context
* **Agent ID**: `/root`
* **Base Model**: `gpt-6-astra`
* **Runtime**: T3 Code / Codex harness

### 📥 User Query
> 解决和 upstream 的冲突，看看上游改了什么，有必要吗。

### 🛠 Changes Overview
**Scope:** macOS 原生引擎与 CLI/MCP、Windows runtime、Linux CLI 参数、回归验证及同步文档。

- 确认 Cherry 当前主线与远端一致；原项目上游包含 75 个独有提交，试合并出现 30 个文件冲突。
- 选择性移植 macOS 点击候选范围、CLI/MCP 实时焦点与选区输入、异常坐标保护、请求 autorelease pool、相对调用文件路径，以及 macOS/Windows 写值读回和 Windows CLI 目标窗口截图；Windows/Linux CLI 的 `set_value` 保留字面空白。
- 保留 Cherry 的 SDK 协议、应用会话、CDP、Go Linux 引擎、品牌和发布体系；不将尚未接入的新功能标记为已合并。
- 为上游新增能力、重复修复和后续需要适配的修复建立[评估记录](../../references/upstream-reconciliation-20261109.md)。
- 补充 patch Changeset、架构与用户发布说明；Windows 函数测试接入 SDK CI。

### 🧠 Design Intent (Why)

上游已有可直接复现的误点击、旧焦点输入、写值假成功和异常几何崩溃修复，值得吸收。Locked Use、虚拟显示器、JS REPL 等是独立产品能力；上游 Python Linux 后端、发布配置也与 Cherry 明显不同，直接整文件覆盖会损失现有实现。

本轮是选择性移植，没有完成完整 upstream merge。focused sheet、可见节点预算、批量 AX 读取、光标跟随及 Windows DPI/子窗口点击保留为后续适配项。

### 验证

- 基线 `swift test`：206 项，1 项按开关跳过，0 失败。
- 修改后 `swift test`：222 项，3 项真实交互测试默认跳过，0 失败。
- 显式运行 `UpstreamNativeInputTests`：2 项独立 AppKit 真实窗口回归通过。覆盖空白/窗口点击不误触其他控件、目标按钮单击、选区与 Unicode 插入、实时焦点切换、焦点不可读、写入失败不重发、延迟/忽略/裁剪写值、清空输入。
- `scripts/run-tool-smoke-tests.sh`：现有 9-tool fixture smoke 与 cursor idle smoke 通过。
- SDK build、类型检查和 47 项客户端/发行包测试通过。
- Windows runtime `go test ./...`、共享 Go runtime `go test -race ./...` 通过；Windows arm64/x64 交叉构建通过。
- Linux 模块全套 Go 测试在规范化临时目录后通过：macOS 默认临时路径含符号链接，先前运行有两项 desktopentry 路径比较失败；设置规范化的 `TMPDIR` 后全部通过。未执行 Linux 真实桌面测试。
- PowerShell 7.6.9 在 macOS 上执行生产函数测试：字面/空字符串、忽略与裁剪写入、只读与单次写入，以及无效/移动/缩放/隐藏/最小化窗口几何通过。此项使用模拟 UIA/Win32 对象。
- 7 项发布脚本测试、版本一致性、文档骨架、仓库卫生、Action SHA 固定与差异检查通过。
- `actionlint` 与未修改的基线对比没有新增诊断；仍有本机工具不识别 `ubuntu-26.04` 和既有 shellcheck 提示，不记为完整 lint 通过。

### 限制

- 当前机器辅助功能已授权，录屏未授权；AppKit 回归显式关闭截图，没有实际验收 SCK 截图。
- Windows 原生桌面截图与跨 DPI 输入尚未验收；模拟 PowerShell 测试和交叉构建不能代替这些验证。
- 远端 CI 未运行，不声称已完成远端同步或合并。

### 📁 Files Modified
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/`：输入、几何、写值读回、CLI 路径和请求资源释放。
- `apps/OpenComputerUse/Sources/OpenComputerUse/MacOSAppAgentProxy.swift`
- `apps/OpenComputerUseLinux/cli_engine.go`：保留 `set_value` 字面参数。
- `apps/OpenComputerUseWindows/`：写值验证、字面参数、目标窗口截图与 Go 回归测试。
- `packages/OpenComputerUseKit/Tests/OpenComputerUseKitTests/`、`scripts/tests/`：单元与独立 AppKit fixture 回归。
- `.github/workflows/sdk-check.yml`、`.changeset/quiet-upstream-input-fixes.md`
- `docs/ARCHITECTURE.md`、`docs/QUALITY_SCORE.md`、`docs/releases/feature-release-notes.md` 与同步评估记录。

### 2026-11-13：新增能力复评

用户认为 Locked Use、虚拟显示器、后台操作、query/select_text、按需观察、截图配置和效率改进都重要，要求进一步核对。

- 刷新 Cherry 主线至 `2d9bd95`、upstream 至 `bef6c05`；保留本地 `cb192a1`，未合并或改动运行时代码。
- 逐项核对当前协议与原生调用路径：SDK 已有不截图、`perform()` 和结构化权限查询；缺少 query/select_text、纯截图模式、非 PNG 格式及后台窗口/锁屏能力。
- 核对上游真实测试记录和当前 PR/issue：虚拟屏显示排列修复 #114 尚未合并；Karabiner #142 仍开放；新暗屏修复 #187 的签名完整锁屏流程尚未实测。
- 新增[能力接入复评](../../references/upstream-feature-adoption-20261113.md)，修正“第一批只修 bug”可能被理解成长期排除能力的表述，给出 SDK 适配、生命周期归属与分批验收方案。
- 本轮为源码评估，没有安装系统组件、触发锁屏或创建显示器；`make check-docs`、修改文档的相对链接检查和 `git diff --check` 通过。未重跑运行时测试，也不把上游验证记录当作 Cherry 的实机验收。


### 2026-11-13：用户确认后的能力接入

用户确认“好的，开始接入”。先整合 Cherry `2d9bd95`，保留此前修复与应用图标 API，再按功能移植 upstream `bef6c05`；虚拟屏布局与自动移窗恢复另固定取自 PR #114 的 `3dedad7`。不是完整 upstream merge，JS REPL、DeepSeek Harness、上游 Python/Linux 与发布流程未引入。

- macOS CLI/MCP 增加 query/select_text，共 11 个工具；SDK 查询结果进入应用会话快照，文本选择进入 act/perform，继续严格校验 appSessionId/snapshotId/elementId。
- 三端按需观察真正跳过未请求的 AX/截图通道；macOS 支持 PNG/JPEG/无损 WebP、尺寸和 JPEG 质量，SDK 返回实际 MIME，CLI 使用 Cherry 独立配置。新增 doctor JSON 诊断。
- 接入 AX 批量读取、可见控件预算、密码值保护、窗口光标跟随、后台窗口及 sky_key。Windows 保留 Cherry HWND/UWP/CDP 路由，新增 PID/句柄/完整元素身份校验和动作已完成但观察失败的区分；Linux 将预算算法适配到现有 Go AT-SPI 引擎。
- macOS SDK 显式启用任务虚拟屏/Locked Use；停止应用只释放所属资源，任务关闭恢复窗口并确认保护释放。移窗前记住原位置，停放和恢复均要求确认；未确认清理返回 CLEANUP_FAILED。
- Broker、Guardian、installer、认证插件改用 Cherry 身份/路径，正式 Developer ID 包捆绑四组件并校验签名和架构；构建不安装服务。libwebp 精确依赖与许可证随制品分发。
- 真实 AppKit 回归发现无 AXIdentifier 的 legacy 文本控件返回通用 failure，修复这一可选属性的兼容性；已知标识消失及角色/名称变化仍拒绝操作。原有坐标命中测试使用合成 1:1 图像帧，只证明输入，不冒充 SCK 截图验证。
- 协议 schema、生成器、TS 客户端、Swift/Go 分发、既有 CI、minor Changeset、架构/SDK/安全/发布说明同步更新。

验证结果：

- 显式开启独立 AppKit 回归后的完整 Swift 测试：536 项，9 项特殊实机测试跳过，0 失败。三个 AppKit 回归包含真实 SDK query → selectText → typeText 的 Unicode 选区替换、实时焦点/写值读回、空白/窗口点击不误触。
- 11-tool 注册、原有工具和 select_text headless MCP smoke，以及 cursor idle smoke 通过。
- SDK 构建/类型检查与 52 项测试、7 项原生私有会话/断连清理协议测试通过。
- 共享 Go runtime 与 Linux 模块 race 测试、Windows Go 测试通过；Windows x64/arm64 编译通过。
- PowerShell 生产函数测试覆盖观察通道、Chromium/密码选区保护、元素身份、窗口/PID 变化、键盘投递、动作与后续观察失败、写值和截图几何；在 macOS 的 pwsh 中运行模拟依赖，非 Windows GUI 证据。
- Cherry ad-hoc app 构建通过；认证插件编译及离线 ABI 拒绝/取消/重放测试通过，没有安装组件或求值系统授权规则。
- 发布脚本、版本一致性、Changeset 状态、修改文档相对链接、文档/仓库卫生、固定 Action SHA 与 diff 检查通过。actionlint 与基线同为 10 条既有诊断，无新增，不记为完整 lint 通过。正式签名、公证和远端 CI 未执行。

剩余边界：本机没有 Developer ID 证书和录屏授权，未安装系统组件、触发锁屏或创建虚拟屏。完整锁屏/暗屏/物理接管、安装升级卸载、多物理显示器/跨 Space、实际 SCK 图片出口及 Windows/Linux GUI 留作独立验收，已登记到技术债。源码接入与这些实机结果分别陈述。

实现与调用说明见[新增桌面能力](../../design-docs/upstream-desktop-capabilities.md)，完整过程见[执行计划](../../exec-plans/completed/20261113-upstream-feature-adoption.md)。
