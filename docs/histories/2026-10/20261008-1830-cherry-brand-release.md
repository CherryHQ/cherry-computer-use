## [2026-10-08 18:30] | Task: Cherry Computer Use 品牌与发布迁移

### Execution Context

- Agent: Codex（主 Agent）
- Runtime: Conductor workspace

### User Query

在现有 SDK PR 上新增 stack PR，将品牌、Bundle ID、签名和 npm publish 换成 Cherry Studio 的方式；用户指定 Bundle ID 为 `com.cherryai.ComputerUse`。

### Changes Overview

- 产品名改为 Cherry Computer Use，复用 Cherry Studio 图标；正式 / dev Bundle ID 分别为 `com.cherryai.ComputerUse` / `.dev`。
- 同步 app 打包、权限发现、SDK/native 查找、CLI/plugin 文案与 installer。独立 Cherry socket 和 turn-ended 通知避免使用上游 agent。
- SDK 保持 `@cherrystudio/computer-use`，增加 `@cherrystudio/computer-use-cli` workspace 和 `cherry-computer-use` 命令；保留原 CLI aliases，停止发布上游三个 npm 包。
- Changesets 管理版本 PR 和 npm 发布，不创建 GitHub Release。沿用 Cherry Studio 的 `NPM_TOKEN`、`CSC_LINK` / `CSC_KEY_PASSWORD`、`APPLE_ID` / `APPLE_APP_SPECIFIC_PASSWORD` / `APPLE_TEAM_ID` 名称。
- 正式发布导入临时证书，检查团队身份、签名、公证、staple 和 Gatekeeper，通过后才打包发布；缺凭据直接失败。旧 tag / Cursor Motion 自动 release 入口移除，实验脚本仍保留。
- Changesets 2.x 依赖检查产生高危传递依赖告警，改用修复后的 3.0.3 / Action v2，Action 固定 SHA。保留 npm workspaces，不迁移项目包管理器。
- 同步发布指南、架构与使用文档，保留上游 MIT 和第三方声明；新增版本同步与发布回归测试。

### Validation

- `swift test`：195 tests，1 skipped，0 failures。
- `npm run sdk:check` 和 `npm run sdk:test`：通过，SDK 36 tests。
- Linux 与 Windows Go modules 的 `go test ./...`：在 macOS 上通过，不代表真实 Windows/Linux GUI 覆盖。
- `npm run release:check`：3 tests，通过缺失 artifact、scoped 包入口、版本同步与缺失签名凭据的失败路径。
- `npm run changeset:status`、隔离目录的 Changesets version cycle 通过；CLI 0.3.5 → 0.3.6、SDK alpha → 0.1.0 的计划正确，同步 native/plugin/lockfile；隔离验证关闭 changelog API，未验证远端 GitHub changelog 生成。
- ad-hoc dev app 和 universal release app 构建、Bundle ID、签名校验通过。
- SDK / CLI tarball 构建、内容审计、CLI workspace prepack 和隔离安装通过；新命令与 `ocu` 兼容入口正常；tarball 包含 universal macOS 和四个 Go artifacts。
- `actionlint`、workflow SHA pinning、文档骨架、shell syntax、`git diff --check` 通过。
- `npm audit --omit=dev` 无告警；完整 audit 剩余既有 esbuild 低危开发依赖告警。

### Boundaries

没有实际 npm publish、Apple Developer ID 签名、公证或 GitHub Release；本机产物为 ad-hoc。查询时仓库级 secrets 列表为空，CI 仍需确认组织或仓库 secrets 配置及发布权限。Cherry Studio 宿主当前引用旧 app 路径，本轮仅把链接仓库作为参考，消费方路径迁移需在宿主完成。SDK 独立平台包和 Windows 签名未在本轮新增。
