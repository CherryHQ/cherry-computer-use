## [2026-10-09 15:30] | Task: 规划 Linux 共用引擎迁移

### Execution Context

- Agent ID: `/root`
- Base Model: GPT-6
- Runtime: Codex / Conductor

### User Query

> 开始计划 Linux 如何复用上游 runtime，或将 Python 实现迁移。

### Changes Overview

Scope: Linux SDK / CLI/MCP 架构计划，仅文档。

- 比较直接复用 Python、SDK 单独迁移和双入口共用 Go 引擎。
- 推荐从现有 Go AT-SPI/X11 后端提取共用引擎，分阶段迁移 Python 的必要能力。
- 明确语义动作、全局输入、Wayland 的不同边界；记录双入口验收和 Python 删除条件。

### Design Intent

保留已有无 Python 部署成果，同时消除 SDK 与 CLI/MCP 两套原生实现的长期维护成本。推荐方案仍需原生接口与输入清理实验，不把规划当作已实现能力。

### Files Modified

- [执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)

### Validation

- 文档骨架、所增文档相对链接及空白检查。
- 未修改运行时代码；Linux 能力迁移尚未实施。

## [2026-10-09 15:57] | Task: 实现 Linux 共用引擎里程碑 1、2

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 继续实现 PR（按计划完成里程碑 1 + 2）。

### Changes Overview

Scope: `apps/OpenComputerUseLinux`、协议测试与相关文档。

- 新增 `internal/desktop` Go AT-SPI/X11 引擎，SDK 与 CLI/MCP 共用发现、窗口选择、树、截图和语义动作；X11 截图从 SDK 文件移入引擎。
- SDK 改为薄适配并实现 `ActionDriver`：语义点击、次级动作、`setValue`、`typeText`；全局输入动作在派发前拒绝。
- CLI/MCP 的 `list_apps`、`get_app_state` 和语义动作改用引擎，文本树格式与元素索引保持不变；`runtime.py` 缩减为只发送全局键鼠输入的 helper，修饰键在异常时也会释放。
- 新增 GTK fixture、可选真实桌面引擎测试与 `protocol/linux-desktop.test.mjs`；更新原生协议测试中的 Linux 能力断言。

### Design Intent

按计划先让两个入口调用同一份原生实现，同时不静默减少 CLI 能力：尚未迁移的全局输入保留旧路径，但由 Go 在派发前决定路线和坐标，避免失败后自动切换后端。语义动作统一执行前校验、单次派发与读回确认，修正旧实现“追加到第一个输入框”和 Unicode 长度按字符计的问题。

### Files Modified

- [Linux 引擎](../../../apps/OpenComputerUseLinux/internal/desktop/)
- [SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[CLI 适配](../../../apps/OpenComputerUseLinux/cli_engine.go)、[全局输入 helper](../../../apps/OpenComputerUseLinux/runtime.py)
- [Linux 动作测试](../../../protocol/linux-desktop.test.mjs)、[原生协议测试](../../../protocol/native.test.mjs)
- [执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)、架构、SDK/协议 README 与 changeset

### Validation

- Go vet、race 单测、Python helper 单测、原生协议测试通过。
- 真实 GNOME Wayland 桌面的引擎、SDK 与 CLI 测试通过，CLI 输出与 Python 版本对比一致；X11 截图、无 Python 容器、x64 与 CLI 全局输入路径未在本轮重跑。
