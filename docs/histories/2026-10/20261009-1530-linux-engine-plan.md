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
- [SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[CLI 适配](../../../apps/OpenComputerUseLinux/cli_engine.go)、全局输入 helper `runtime.py`（后续已删除）
- [Linux 动作测试](../../../protocol/linux-desktop.test.mjs)、[原生协议测试](../../../protocol/native.test.mjs)
- [执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)、架构、SDK/协议 README 与 changeset

### Validation

- Go vet、race 单测、Python helper 单测、原生协议测试通过。
- 真实 GNOME Wayland 桌面的引擎、SDK 与 CLI 测试通过，CLI 输出与 Python 版本对比一致；X11 截图、无 Python 容器、x64 与 CLI 全局输入路径未在本轮重跑。

## [2026-10-09 16:15] | Task: 实现 Linux 全局输入迁移并删除 Python

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 实现下一步（里程碑 3，并在验证后完成里程碑 4 的代码部分）。

### Changes Overview

Scope: `apps/OpenComputerUseLinux`、协议测试、CI 与相关文档。

- 在隔离 Xephyr 会话中验证全局输入方案：XTEST 可用；X server 不释放断开客户端按住的键；原 Python 修饰键与 CJK 打字路径有缺陷。
- 新增引擎 XTEST 输入（按键、打字、指针点击、拖拽、翻页），键盘要求目标窗口持有焦点、指针要求落点属于目标窗口；缺失字符临时映射后恢复。
- 新增 `input-guard` 子进程：按下前登记，owner 任意方式退出时只释放其仍按住的输入。
- SDK 在 X11 且 `allowGlobalInput: true` 时开放这些动作，Wayland 报告 unsupported；CLI 全部改走引擎，删除 `runtime.py`、其测试与嵌入。
- 新增 `testdata/x11-session.sh` 隔离会话，CI Linux job 在 Xvfb 中运行全局输入与 SDK 桌面测试。

### Design Intent

按计划先实验再定进程布局。实验证明 XTEST 按键在客户端死亡后会卡住，所以清理者必须独立于 owner；guard 只做清理，执行仍在进程内，避免为每个动作跨进程通信。焦点与遮挡检查让输入不会落到其他窗口，Wayland 下拒绝而不是假装送达。

### Files Modified

- [输入与 guard](../../../apps/OpenComputerUseLinux/internal/desktop/input.go)、[guard](../../../apps/OpenComputerUseLinux/internal/desktop/guard.go)、[键位](../../../apps/OpenComputerUseLinux/internal/desktop/keys.go)
- [SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[CLI 适配](../../../apps/OpenComputerUseLinux/cli_engine.go)、[隔离会话脚本](../../../apps/OpenComputerUseLinux/testdata/x11-session.sh)
- [CI](../../../.github/workflows/sdk-check.yml)、[执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)、架构、SDK/协议 README 与 changeset

### Validation

- 隔离 X11 中引擎全局输入测试、SDK 两个桌面测试与 CLI 无 Python 运行通过；CI 步骤主体本地照跑通过。
- 真实 Wayland 桌面只验证了拒绝路径与语义动作，未向其注入全局输入。x64、平台 tarball、带窗口管理器的 X11 与非 GTK 应用未验证。
