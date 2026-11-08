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
