## [2026-10-08 16:00] | Task: macOS SDK 改为复用上游引擎

### 🤖 Execution Context
* **Agent ID**: `Claude Code`
* **Base Model**: `claude-opus-5-5`
* **Runtime**: `Conductor workspace, macOS`

### 📥 User Query
> Agent 在飞书上使用 Computer Use 时绑错窗口、主窗口树为空、截图失败，且只能点击。查明原因是 SDK 用的是只支持左键语义点击的并行精简后端；要求直接复用上游引擎。

### 🛠 Changes Overview
**Scope:** `packages/OpenComputerUseKit`、`apps/OpenComputerUse`、`protocol`

**Key Actions:**
- **拆分引擎动作**: `ComputerUseService` 的七种动作拆成接收快照的 `perform*` 与原有 MCP 包装，MCP 行为与参数校验顺序不变。
- **结构化元素**: `ElementRecord` 记录父节点、名称和值，渲染器报告节点/深度截断。
- **SDK 适配层**: `MacOSSDKDesktop` 的观察与动作改为调用引擎，删掉自建的树遍历、窗口选择和截图匹配；保留会话、归属与停止；快照返回引擎渲染的 `tree.text`。
- **修复停止应用会话**: proxy 把 `stopAppSession` 结果中的 `cleanup: complete` 误认为 shutdown 确认并退出，改为按 shutdown 请求 ID 匹配。

### 🧠 Design Intent (Why)
首个 SDK 切片绕开引擎直接调用平台 API，丢掉了引擎已有的焦点窗口选择、Chromium/Electron 辅助功能模式、按窗口 ID 截图和其余动作。改为适配层后 SDK 与 MCP 共用同一引擎。

### 📁 Files Modified
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/ComputerUseService.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/AccessibilitySnapshot.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/SDKDesktop.swift`
- `apps/OpenComputerUse/Sources/OpenComputerUse/MacOSSDKRuntime.swift`
- `protocol/schema.json`
- `protocol/native.test.mjs`
- `docs/design-docs/computer-use-runtime.md`
- `docs/design-docs/computer-use-sdk.md`
