# Cherry Computer Use 发布指南

## 发布边界

本仓库沿用 Cherry Studio 的 Changesets 版本 PR / npm 发布方式。日常开发只构建和验证；不要用真实 publish 验证脚本。合并版本 PR 表示批准公开发布。

当前只公开发布 SDK；CLI 暂时不发布：

| 包 | 用途 | 版本源 |
| --- | --- | --- |
| `@cherrystudio/computer-use` | TypeScript SDK | `packages/sdk/package.json` |
| `@cherrystudio/computer-use-cli` | private，仅本地打包 native runtime 和 MCP CLI | `packages/cli/package.json` |

不再发布上游 `open-computer-use`、`open-computer-use-mcp`、`open-codex-computer-use-mcp` 包。`cherry-computer-use` 是新 CLI 命令，原 `open-computer-use`、`ocu` 和 MCP aliases 保留兼容。SDK 的六个平台专用包仍未交付，使用本地构建的 runtime 需传显式 `runtimePath`。CLI workspace 和生成的 staging manifest 均带 `private: true`，并被 Changesets ignore；不会被版本 PR 或 publish 自动发出。

## 品牌与 macOS 身份

- 正式 app：`Cherry Computer Use.app`，Bundle ID `com.cherryai.cherrystudio.computer-use `。
- 开发 app：`Cherry Computer Use (Dev).app`，Bundle ID `com.cherryai.cherrystudio.computer-use .dev`。
- 图标来自 Cherry Studio 的 `build/icons/1024x1024.png`，来源见 `assets/app-icons/README.md`。
- 可执行文件仍叫 `OpenComputerUse`；Swift target、CLI 参数和 `OPEN_COMPUTER_USE_*` 运行时配置不改名。
- Bundle ID 是独立 helper 的身份，不复用 Cherry Studio 主程序身份。旧上游 TCC 权限不会自动迁移，需要向新 app 授予辅助功能和屏幕录制权限。
- app-agent 默认 socket 和 turn-ended 通知已隔离到 Cherry，避免连接或清理上游 app 的会话。
- 宿主打包、dev symlink 和显式 `runtimePath` 应改用新 app 路径；不应继续假设 `Open Computer Use.app`。Cherry Studio 消费方迁移属于宿主集成工作。

## Changesets 流程

1. 实现公开行为变更后运行 `npm run changeset`，选择 SDK 包并写发布说明；CLI 不参与当前版本计划。
2. 合并到 `main` 后，[release.yml](../../.github/workflows/release.yml) 创建或更新 `chore: version packages` PR。
3. 版本 PR 执行 `npm run changeset:version`：更新 package manifests / changelogs、同步 CLI 对应的 plugin 与 Swift/Go native 版本，并更新 npm lockfile。
4. 审核版本和 changelog 后合并版本 PR。没有待处理 changeset 时 Action 执行 `npm run changeset:publish`，构建并发布尚未发布的 SDK 版本，不构建 CLI/native，也不执行 Apple 签名与公证。
5. 检查 Actions 和 npm registry 上的目标版本。与 Cherry Studio 一样，`create-github-releases: false`，不自动创建 GitHub Releases。上游 semver tag 和 Cursor Motion DMG 不再触发公开发布；实验室 DMG 仍可本地构建。

使用 Changesets 3 / Action v2，保留 Cherry Studio 的版本 PR、`NPM_TOKEN` 认证和 workspace 发布模型。2.x 的旧依赖链有已知漏洞，本仓库不复制该依赖版本。Action 固定 SHA；项目仍使用 npm workspaces，不额外迁移 pnpm。

## CI 配置

仅 `CherryHQ/cherry-computer-use` 的 `main` push 或在 main 上手动运行会进入发布流程；fork、PR、tag 不触发。需要开启 GitHub Actions 创建 PR 的权限，当前在 Ubuntu runner 上仅发布 TypeScript SDK，只需要 `NPM_TOKEN`。独立的 `bash scripts/build-signed-release.sh` 仍保留给显式 native 制品构建，使用其余 Apple secrets（名称与 Cherry Studio 相同）：

| Secret | 用途 |
| --- | --- |
| `NPM_TOKEN` | 有权发布 `@cherrystudio/computer-use` 的 npm token |
| `CSC_LINK` | Developer ID Application 的 p12：base64、HTTPS URL 或本地绝对路径 / file URL |
| `CSC_KEY_PASSWORD` | p12 密码 |
| `APPLE_ID` | Apple 开发者账号 |
| `APPLE_APP_SPECIFIC_PASSWORD` | 该账号的 app-specific password |
| `APPLE_TEAM_ID` | 与签名证书匹配的团队 ID |

无需旧 `ENABLE_LEGACY_RELEASE` variable、`OPEN_COMPUTER_USE_CODESIGN_P12_*` 或 `APPLE_NOTARY_*` secrets。这里只描述变量名，不将证书、token 或密码写入仓库。组织 secrets 需要授权给这个仓库；配置是否存在不代表实际签名 / 发布已通过。

`build-signed-release.sh` 导入临时 keychain，要求 Developer ID Application 与团队 ID 匹配，使用 hardened runtime 和 timestamp 签名 universal app。随后用 `notarytool` 提交 app zip，确认 Accepted 后 staple / validate / Gatekeeper assess，再重新 stage npm 内容。退出时清理 p12 和 keychain。正式流程缺少凭据、签名失败或公证失败均停止，不回退 ad-hoc。

## 本地验证与打包

```sh
npm ci --ignore-scripts
npm run release:versions:check
npm run release:check
npm run sdk:check
npm run sdk:test
swift test
OPEN_COMPUTER_USE_CODESIGN_MODE=adhoc npm run npm:pack
```

最后一条只在 macOS 运行，构建 universal app 和四个 Go 二进制，产出：

- `dist/release/npm/cherrystudio-computer-use-<sdk-version>.tgz`
- `dist/release/npm/cherrystudio-computer-use-cli-<cli-version>.tgz`
- `dist/release/release-manifest.json`

本地 ad-hoc 构建不等价于正式 Apple 签名或公证。SDK 的 `prepack` 构建类型和 ESM/CJS；CLI 的 `prepack` 只复制已经构建的完整 artifacts，缺失任一平台二进制就失败。CLI tarball 保留 private 标记，只供本地安装，不发布 registry。Apple 签名脚本独立保留，SDK publish 不调用它，也不要求 Apple 凭据。

`npm run npm:build` 可独立 stage CLI 到 `dist/npm/computer-use-cli`；已有完整产物时用 `npm run npm:build -- --skip-build`。不要把 staging 路径指向源码目录。旧 `scripts/npm/publish-packages.mjs` 只转发到 Changesets 正式流程，已删除上游包发布和认证 fallback。

仅在明确授权公开发布且配置完 `NPM_TOKEN` 时运行 `npm run changeset:publish`。这一命令会验证 SDK，再调用 Changesets publish；当前只有 SDK 是可发布 workspace，不用于本地 smoke test。后续恢复 CLI 发布需显式移除 private / ignore、补回 CLI changeset，并重新接上签名和公证构建，不能只修改包名。

## 故障排查

- 版本不同步：运行 `node scripts/npm/sync-versions.mjs`，检查并提交结果；不要仅手动改 tag。
- 缺失平台 artifact：在 macOS 运行完整 `npm run npm:build`，不要用 `--skip-build` 绕过构建。
- 签名 / notarization：检查团队 ID 与证书是否一致、Apple 凭据是否有效；失败时不要降级后发布。
- npm 403：检查 `NPM_TOKEN` 对 Cherry scope 的权限、版本是否已经发布。Changesets 会识别已存在版本，不覆盖旧版本。
- Action 未创建版本 PR：检查 main 上的 changeset、Actions 的 PR 权限及仓库条件。
- 查看失败日志：`gh run list -R CherryHQ/cherry-computer-use --workflow release.yml`，再运行 `gh run view <run-id> --log-failed`。

历史 GitHub release notes 与 Cursor Motion 文档保留为上游版本记录，不再作为 Cherry npm 的版本源。
