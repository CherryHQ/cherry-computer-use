## [2026-10-09 15:00] | Task: Windows SDK 复用原有引擎并修复中文输入

### Execution Context
- Agent: Codex
- Runtime: macOS 开发工作区，Windows 11 ARM64 Parallels 验证

### User Query
确认 Cherry Studio #21405 的 Windows SDK 使用了自行新增的精简通路后，要求直接在 Computer Use SDK 工作区修复并复用上游引擎。

### Changes
- Windows SDK 的观察使用原有树记录；公共动作使用同一个 `Invoke-Operation`，去掉单独的点击实现，接通七种动作及坐标/多次点击。
- 共享 Go 会话增加完整动作驱动入口、严格参数校验、坐标与辅助动作结构，保留快照隔离、停止、已完成动作回执和不确定副作用防重放；Linux 能力不变。
- stdin 明确 UTF-8；解析纳入协议错误处理，坏 JSON 返回 `INVALID_ARGUMENT / effect=none`，不因解析失败关闭会话。
- 树记录区分完整身份名称与截断显示，记录父节点和截断原因；动作前复核原生身份、窗口和元素位置。
- SDK 不启用全局输入或环境变量中的抢焦点文本回退；定向按键和拖拽通过 finally 释放，重复输入支持取消检查。
- 新增中文 Windows bridge 测试、会话回归和真实 WinForms 七动作测试，并接入 Windows CI；补充 patch changeset 与文档。

### Validation
- Go race、Windows/Linux Go 测试、Go vet 与 Windows ARM64/x64 编译通过。
- Windows 11 ARM64 运行 Go 会话/bridge 全组通过，包括 CP936 Unicode 解码、坏载荷和 worker/子进程清理。首次冷启动清理用例等待超时，定向及最终全组重跑通过。
- SDK 构建、类型检查和 44 项客户端/打包测试通过。
- Windows 原生协议 4 项通过（3 项 macOS 专属跳过）；原有桌面 fixture 通过；新增七动作 fixture 通过，观察实际控件值和窗口收到的消息。
- 使用 Electron Node 24.16 模式和显式 ARM64 runtimePath；未覆盖已安装 Cherry Studio，未运行远端 CI。
- 文档骨架、changeset 选择与 diff 检查通过。

### Boundaries
本变更通过 PR 交付，不包含 npm 发布。多窗口选择、GPU 空白截图、第三方应用的后台输入接受程度仍是现有引擎边界；未把 fixture 通过等同于真实应用的焦点/物理鼠标验收。

执行与验证细节见 [完成计划](../../exec-plans/completed/20261009-windows-sdk-engine.md)。
