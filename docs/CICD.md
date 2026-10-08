# CI/CD 说明

日常验证由 [sdk-check.yml](../.github/workflows/sdk-check.yml) 负责；正式发布由 [release.yml](../.github/workflows/release.yml) 负责。

## Cherry 发布入口

发布采用与 Cherry Studio 一致的 Changesets 版本 PR / npm 模型，仅在本仓库 main 上运行。当前只发布 SDK `@cherrystudio/computer-use`；CLI `@cherrystudio/computer-use-cli` 为 private workspace 且被 Changesets ignore，暂不发布，不再向上游未 scoped 包发布，也不自动发布 Cursor Motion DMG 或创建 GitHub Release。

`npm run changeset:version` 更新 SDK、六个平台包依赖和原生版本；CLI/plugin 版本保持独立。版本 PR 合并后，发布 workflow 调用 [sdk-distribution.yml](../.github/workflows/sdk-distribution.yml)，在六个原生 OS/CPU runner 上安装同一 SDK tarball、自动选择平台包并验证启动/关闭。macOS 正式构建要求签名、公证和安装后验证；PR 只使用 ad-hoc。Intel macOS 显式选择 Xcode 26.2，避免 runner 默认 Swift 6.1 不满足项目的 Swift 6.2 要求。

全部平台通过后，先发布六个平台包，再发布 SDK。发布门禁检查产物完整性、源码提交、同一 SDK 哈希、实际安装验证记录和 macOS 签名标记。CLI 保持 private；不再允许只构建 JS 就发布 SDK。SDK 0.1.0 缺少平台分发，本次 patch changeset 修复这一问题。流程、重试边界和 secrets 见 [发布指南](releases/RELEASE_GUIDE.md)。

## SDK 与原生桌面检查

[sdk-check.yml](../.github/workflows/sdk-check.yml) 在 macOS、Windows、Linux 分别检查 Node 24 SDK 构建、类型和 tarball 消费，并构建本平台 runtime，运行 [native contract tests](../protocol/native.test.mjs)。macOS 另跑 Swift session 测试和 ad-hoc `.app` 构建，Windows/Linux 使用共用 Go session；Windows 增加 Job Object 取消/宿主退出单测，并启动 WinForms counter 执行 SDK 桌面测试；Linux 在隔离的 [Go AT-SPI/X11 容器](../experiments/LinuxNativeProbe/README.md)执行同一桌面测试及独立 probe。这条检查不发布包；macOS GUI、Wayland 与 Electron 制品另行验收。

SDK 与 native 原来的六个 job 合并为三个平台 job，每个平台共享 checkout、npm 安装和 SDK 构建。`npm run test:built --workspace @cherrystudio/computer-use` 复用产物并保留 tarball 测试需要的 npm 环境；日常 `npm run sdk:test` 仍先构建。SDK 测试位于末尾，在构建成功且未取消时即使前面的原生检查失败也会执行，任一检查失败都会使 job 失败。非 Windows 平台只执行一次带 race detector 的共享 Go 测试。

npm 与 Go 使用 Actions 缓存；Go 缓存键覆盖 Windows、Linux 和 probe 的依赖锁文件。同一 PR 或分支的新运行取消旧运行，三个平台保留 `fail-fast: false`。PR 与 main push 保留路径过滤，另外支持手动运行。检查名称改为 `SDK and native (<runner>)`，使用旧 `sdk` / `native` job 名称的分支保护需相应更新。

Windows x64 本机真实桌面、原生协议、类型检查已通过。SDK 的 EOF 和 broken-input 故障注入使用独立管道，避免 Windows 上 Node 标准流的句柄生命周期阻止真实断连；测试仍检查原有错误码及进程清理，不跳过 Windows 用例。三平台合并矩阵由 Actions 验证。

## 发布改动验证

SDK matrix 同时运行 `npm run release:versions:check` 和 `npm run release:check`，检查版本同步、CLI staging、缺失制品与签名凭据失败路径。品牌资产、CLI workspace、发布脚本与 workflow 变更会触发矩阵。

Action 必须固定 commit SHA。远端三平台通过、本地 ad-hoc 产物、Apple 正式签名 / 公证和 npm registry 发布分别报告，不能互相替代。
