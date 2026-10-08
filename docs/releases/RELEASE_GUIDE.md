# Cherry Computer Use 发布指南

## 发布边界与版本

采用 Cherry Studio 的 Changesets 版本 PR / npm token 模型。合并版本 PR 才表示批准公开发布；日常开发通过本地 registry 验证 tarball，不能用真实 publish 当测试。

`0.1.0` 已发布，但只有 JS/types，必须提供 `runtimePath`。本次 patch changeset 修复后端分发；已发布版本不会被覆盖或撤包。

| 包 | 交付内容 | 版本源 |
| --- | --- | --- |
| `@cherrystudio/computer-use` | ESM/CJS SDK、类型与六项精确版本的 optionalDependencies | `packages/sdk/package.json` |
| `@cherrystudio/computer-use-{darwin,win32,linux}-{arm64,x64}` | 对应 OS/CPU 的原生 helper，不暴露 npm CLI 命令 | SDK 版本 |
| `@cherrystudio/computer-use-cli` | private，仅本地 CLI/MCP 打包 | `packages/cli/package.json` |

正式平台包在构建时生成到 `dist/sdk-packages`。`packages/sdk-native/*` 是六个 private 开发占位 workspace，只提供与 SDK 同步的名称/版本，保证尚未发布的新版本也能执行 `npm ci`；通过 Changesets fixed group 与 SDK 一起版本化（privatePackages.version=true、tag=false），不包含运行时、OS/CPU 安装限制，绝不直接发布。正式 staging manifest 才带 OS/CPU 和原生产物。安装 SDK 时 npm 根据 `os` / `cpu` 选择平台包。不要使用 `--omit=optional`；SDK 不在运行时下载或执行安装脚本。缺失平台包时明确报 `RUNTIME_NOT_FOUND`。CLI 保留 private / Changesets ignore，不公开发布。

SDK 包版本同时驱动 Swift/Go 的版本常量和 `.app` 元数据。CLI 的 plugin manifest 保持 CLI 版本，两者由 `sync-versions.mjs` 分别同步。

## macOS 身份与签名

- 正式 app：`Cherry Computer Use.app`，Bundle ID `com.cherryai.cherrystudio.computer-use`。
- 开发 app：`Cherry Computer Use (Dev).app`，Bundle ID `com.cherryai.cherrystudio.computer-use.dev`。
- 修正旧字符串误入的空格；命名空间保留。旧副本的 TCC 授权可能需要重新授予。
- 可执行文件名仍是 `Contents/MacOS/OpenComputerUse`，宿主不能用自己的权限代替 helper 权限。
- PR 验证使用 ad-hoc 签名。正式发布必须使用 Developer ID Application、hardened runtime、timestamp；公证 Accepted 后 staple，并检查 codesign / stapler / Gatekeeper。没有凭据或验证失败时停止，不降级。
- 签名完成后才生成 npm tarball；安装到全新 consumer 后再次验证签名、公证票据、团队 ID 和 Bundle ID。临时证书与 keychain 在退出时清理。

## 发布流水线

[release.yml](../../.github/workflows/release.yml) 仅在本仓库 main 执行：

1. `prepare` 运行版本同步和发布回归测试，用 Changesets Action 创建/更新版本 PR。SDK 的 patch changeset 进入下一版，六个平台依赖同步为同一版本。此阶段不需要 Apple/npm secrets。
2. 没有待处理 changeset 且 SDK 目标版本未发布时，调用 [sdk-distribution.yml](../../.github/workflows/sdk-distribution.yml)。Registry 非 404 错误直接失败，不能把权限/网络错误当作未发布。
3. Ubuntu 构建并打包唯一的 SDK tarball。六个原生 runner 分别构建平台后端；macOS 两种架构分别签名、公证。每个平台下载同一 SDK tarball，在临时 registry 中安装它，让 npm 自动选择真实平台 tarball，验证 ESM/CJS 的 `ComputerUse.start()`、能力/权限查询及确认关闭。无需开发 checkout 或 `runtimePath`。
4. 六个 job 全部通过才上传平台 tarball 与验证记录。`publish` 下载本次 run 的制品，校验名称、版本、OS/CPU、源码提交、原生与 SDK tarball 哈希、安装验证结果和 macOS 正式签名标记，之后才允许任何 npm 写入。
5. 先发布六个平台包，等待 registry 可见，再发布已验证的 SDK tarball；使用 `--ignore-scripts` 避免重新打包改变已验收内容，使用 npm provenance。不自动创建 GitHub Release，不发布 CLI 或 Cursor Motion。

部分平台发布成功后失败时，可重跑同一源码提交的 release。只复用版本、目标和源码提交匹配的已发布平台包；不同源码同版本停止，避免混装。签名时间戳可能不同，复用不要求重建包字节相同。SDK 版本已经存在时不能覆盖；完整修复应进入新的 Changeset 版本。

## Secrets

| Secret | 用途 |
| --- | --- |
| `NPM_TOKEN` | 有权创建/发布 Cherry scope 下 SDK 及全部六个平台包的 token |
| `CSC_LINK` | Developer ID Application p12，支持 base64、HTTPS URL、本地路径/file URL |
| `CSC_KEY_PASSWORD` | p12 密码 |
| `APPLE_ID` | Apple 开发者账号 |
| `APPLE_APP_SPECIFIC_PASSWORD` | Apple app-specific password |
| `APPLE_TEAM_ID` | 与证书对应的团队 ID |
| `GITHUB_TOKEN` | Actions 自动提供；用于 Changesets 版本 PR，无需手动添加 |

启用 Actions 创建 PR 的权限。组织 secrets 需要授权本仓库；配置存在不代表正式签名/公证已经通过。PR 不使用上述发布凭据。Windows 当前为未 Authenticode 签名的 Go 可执行文件；本轮不声明 Windows 代码签名。

## 本地验证

```sh
npm ci --ignore-scripts
npm run release:versions:check
npm run release:check
npm run sdk:check
npm run sdk:test
mkdir -p dist/sdk-artifacts
npm pack --workspace @cherrystudio/computer-use --ignore-scripts --pack-destination dist/sdk-artifacts
npm run sdk:runtime:build
npm run sdk:package:verify
```

最后两步针对当前 OS/CPU；macOS 默认 ad-hoc。`npm run sdk:runtime:build -- --signed` 仅在 macOS 显式请求正式签名。运行 `npm run sdk:release:validate` 会要求六套同一源码提交、同一 SDK tarball 的验证记录，并拒绝 ad-hoc macOS 包。构建产物上传为 Actions artifacts，直接下载 `.tgz` 保留可执行权限和 app 签名。

旧 `npm run npm:pack` 仍用于本地 CLI/MCP 打包；它不生成完整 SDK 发布候选集，不能用于公开 SDK 发布。`npm run changeset:publish` 仅接受正式 main 工作流下载的全部已验证制品，不会退回只发 JS SDK。

## 验收边界

六架构安装/启动测试证明原生包选择、打包和会话生命周期；已有 sdk-check 提供 Windows/Linux 桌面 fixture 回归。macOS GUI 权限、Wayland 行为和 Electron ASAR/resources 打包仍需独立验收。正式 Apple 服务、公证与 npm 发布结果应在首次 release 后单独记录，不能用 PR ad-hoc CI 代替。

参考：[npm optionalDependencies/os/cpu](https://docs.npmjs.com/cli/v11/configuring-npm/package-json)、[GitHub 原生 runner 标签](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。
