# Cherry Computer Use 品牌与发布迁移

## 目标与范围

在 SDK PR #1 上新增一层 PR，将产品身份和正式 npm 发布迁移到 Cherry Studio 的约定。
默认名称为 Cherry Computer Use；独立 helper 使用 `com.cherryai.ComputerUse`，开发版加 `.dev`，不复用宿主的 Bundle ID。

## 实施

- [x] 更新 app、权限发现、SDK runtime 路径、CLI/plugin 文案与 Cherry 图标；保留已有 CLI 命令和源代码模块名。
- [x] npm SDK 保留 `@cherrystudio/computer-use`，CLI 使用 `@cherrystudio/computer-use-cli` 作为 private workspace，暂不公开发布；停止向上游三个包发布。
- [x] 接入 Changesets 版本 PR / npm 发布，复用 `NPM_TOKEN`、`CSC_LINK` / `CSC_KEY_PASSWORD`、`APPLE_ID` / `APPLE_APP_SPECIFIC_PASSWORD` / `APPLE_TEAM_ID`。
- [x] 正式 macOS 制品签名、公证、staple 后打包；本地和 PR 验证允许显式 ad-hoc，不冒充正式签名。
- [x] 同步发布文档和 history，完成本地构建、包内容/安装、签名脚本失败路径验证。
- [x] 签名并 sign off 提交，创建以上一层为 base 的 stack PR，核对远端 SHA。

## 约束与风险

- 本轮不触发真实 npm publish、GitHub Release 或修改仓库 secrets。
- Bundle ID 变更需要重新授予 macOS 权限；新旧产品不能共享 app agent socket 或 turn-ended 通知。
- Cherry Studio 当前开发集成引用旧 app 路径，消费方需要迁移到新 bundle 路径；该仓库仅作流程和品牌参考。
- Changesets 当前仅管理 SDK 发布，CLI 被 ignore；原生版本常量、plugin manifest 必须由版本同步脚本保持一致。
- 本机可验证 ad-hoc 制品和打包；Apple 正式签名、公证与 npm 发布需 CI secrets，单独报告未覆盖部分。

## 验收

Swift / Go / SDK 回归通过；本地 app 的 Info.plist、签名、图标一致；tarball 仅包含 Cherry scoped 包；workflow pinning / actionlint 通过；新 PR diff 仅含本层变更。

## 本地验证结果

Swift 195 tests（1 skipped）、SDK 36 tests、发布脚本 3 tests、两端 Go tests 均通过。完成 dev / universal app 构建、Bundle ID 与 ad-hoc 签名检查、两个 scoped tarball 内容审计和隔离安装。Changesets 版本及 lockfile/native 同步的隔离验证通过。Apple 正式签名、公证和 npm 发布保留为 CI 凭据验收，未在本轮触发。

## 交付

已创建 [PR #2](https://github.com/CherryHQ/cherry-computer-use/pull/2)，base 为 `ankara-v3`，与 #1 建立原生 stack #3（#1 → #2）。代码提交已签名并 sign off；远端三平台 CI 排队中，正式发布依赖凭据与后续版本 PR，不在本轮执行。

## 后续范围收窄

按用户“先不发布 CLI”调整：CLI 标记 private 并从 Changesets release plan 移除，生成的本地包同样为 private；SDK 发布不再构建 native 或要求 Apple 凭据。保留本地 native 打包和独立签名脚本。
