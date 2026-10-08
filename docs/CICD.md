# CI/CD 说明

这个模板自带一套不依赖具体语言栈的 CI/CD 骨架。

Cherry fork 的日常验证统一在 `sdk-check.yml`，发布保留在独立的 `release.yml`。
继承的发布流程默认禁用；只有仓库 Actions variable `ENABLE_LEGACY_RELEASE` 设置为字符串 `true` 时才会执行，包括手动触发。启用前应确认既有 npm 包名、发布权限、签名身份与发行说明均适用于目标仓库。这一开关不启用尚未实现的 Cherry SDK 发布。

## 当前 release 入口

- `scripts/release-package.sh`：构建 universal `Open Computer Use.app`，cross-compile Linux / Windows runtime，stage 三个既有 root/alias npm 包；每个包都会内置 macOS app、Linux binaries 和 Windows exes，并暴露 `open-computer-use` / `ocu` 等 npm bin 入口，产出 `dist/release/npm/*.tgz` 与 `dist/release/release-manifest.json`。当前 CI 继续显式使用 ad-hoc signing，保持和此前发布链路一致；本地 debug/dev 构建则允许使用开发机自己的签名身份。
- `scripts/build-cursor-motion-dmg.sh`：本地构建 `Cursor Motion.app` 并封装 `dist/release/cursor-motion/CursorMotion-<version>.dmg`，支持 `native` / `arm64` / `x86_64` / `universal`。
- `scripts/build-open-computer-use-linux.sh`：本地构建实验性 Linux `open-computer-use` binary，支持 `arm64` / `amd64`；release package 会把这两个产物内置进既有 npm 包的 `dist/linux/`。
- `scripts/build-open-computer-use-windows.sh`：本地构建实验性 Windows `open-computer-use.exe`，支持 `arm64` / `amd64`；release package 会把这两个产物内置进既有 npm 包的 `dist/windows/`。
- `.github/workflows/release.yml`：支持 push semver tag 自动发布，也支持手动触发；tag push 时会同时跑 npm release 打包逻辑与 `Cursor Motion` 的 DMG 打包，并把 `.dmg` 上传到对应的 GitHub Releases 页面。`Open Computer Use` 的 npm 产物默认走 ad-hoc signing；如果配置了 `OPEN_COMPUTER_USE_CODESIGN_*` secrets，则会先导入 `Developer ID Application` 证书，再按同一 identity 对 release `.app` 统一签名。`Cursor Motion` 的 DMG 也会复用同一张 `Developer ID Application` 证书签 app；若同时配置 `APPLE_NOTARY_*` secrets，则会在上传前对 `.dmg` 做 notarization 和 staple。

## SDK 与原生桌面检查

[sdk-check.yml](../.github/workflows/sdk-check.yml) 在 macOS、Windows、Linux 分别检查 Node 24 SDK 构建、类型和 tarball 消费，并构建本平台 runtime，运行 [native contract tests](../protocol/native.test.mjs)。macOS 另跑 Swift session 测试和 ad-hoc `.app` 构建，Windows/Linux 使用共用 Go session；Windows 增加 Job Object 取消/宿主退出单测，并启动 WinForms counter 执行 SDK 桌面测试；Linux 在隔离的 [Go AT-SPI/X11 容器](../experiments/LinuxNativeProbe/README.md)执行同一桌面测试及独立 probe。这条检查不发布包；macOS GUI、Wayland 与 Electron 制品另行验收。

SDK 与 native 原来的六个 job 合并为三个平台 job，每个平台共享 checkout、npm 安装和 SDK 构建。`npm run test:built --workspace @cherrystudio/computer-use` 复用产物并保留 tarball 测试需要的 npm 环境；日常 `npm run sdk:test` 仍先构建。SDK 测试位于末尾，在构建成功且未取消时即使前面的原生检查失败也会执行，任一检查失败都会使 job 失败。非 Windows 平台只执行一次带 race detector 的共享 Go 测试。

npm 与 Go 使用 Actions 缓存；Go 缓存键覆盖 Windows、Linux 和 probe 的依赖锁文件。同一 PR 或分支的新运行取消旧运行，三个平台保留 `fail-fast: false`。PR 与 main push 保留路径过滤，另外支持手动运行。检查名称改为 `SDK and native (<runner>)`，使用旧 `sdk` / `native` job 名称的分支保护需相应更新。

Windows x64 本机真实桌面、原生协议、类型检查已通过。SDK 的 EOF 和 broken-input 故障注入使用独立管道，避免 Windows 上 Node 标准流的句柄生命周期阻止真实断连；测试仍检查原有错误码及进程清理，不跳过 Windows 用例。三平台合并矩阵由 Actions 验证。

## 设计原则

这套默认流水线的目标，是在项目真正成形前先把交付链路搭起来，而不是假装已经知道未来项目该怎么 build 和 deploy。

当新项目的技术栈确定后，你应该继续在 `scripts/release-package.sh` 这条真实构建链路上扩展，而不是另起一套平行流程。

所有 GitHub Actions 都已经 pin 到 commit SHA。后续升级 action 时，也要继续保持这个约束。

## 推荐接入顺序

1. 保留 `ci.yml`，作为仓库的基础门禁。
2. 在 `scripts/ci.sh` 里继续叠加项目自己的验证命令。
3. 在 `scripts/release-package.sh` 已有的真实构建基础上继续扩展 release 产物。
4. 技术栈和环境稳定后，再补具体的部署 job。
5. 即使交付方式变化，SBOM 和 provenance 这类供应链能力也建议保留。

## 默认 release 产物

当前 release 流水线会产出：

- `dist/release/release-manifest.json`
- `dist/release/npm/open-computer-use-<version>.tgz`
- `dist/release/npm/open-computer-use-mcp-<version>.tgz`
- `dist/release/npm/open-codex-computer-use-mcp-<version>.tgz`
- `dist/release/cursor-motion/CursorMotion-<version>.dmg`
- GitHub Actions 中上传的 npm release artifact
- GitHub Releases 中和 tag 对齐的 `CursorMotion-<version>.dmg`

也就是说，即使项目还没进入更复杂的部署阶段，仓库现在也已经同时具备了一条真实可复用的 npm 制品封装链路，以及一条由 git tag 驱动的 macOS app DMG 交付链路。
