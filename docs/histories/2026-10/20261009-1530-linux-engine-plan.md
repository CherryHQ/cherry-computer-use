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
