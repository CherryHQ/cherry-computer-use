## [2026-09-19] | Task: 合并 fork 的 Computer Use CI

### 🤖 Execution Context
* **Agent ID**: `/root`
* **Base Model**: GPT-6
* **Runtime**: Codex desktop

### 📥 User Query
> 这个因为是 fork 的，然后这个 CI 需要优化合并一下。

### 🛠 Changes Overview
**Scope:** GitHub Actions、SDK 测试入口、CI 文档。

- SDK/native 从两组三平台矩阵合并为一组三平台矩阵，共享依赖安装和 SDK 构建。
- 新增 `test:built` 以复用构建结果，保留 npm 环境及全部测试断言。
- 加入并发取消、手动运行、Go 依赖缓存，去除非 Windows 重复执行的非 race 测试。
- 继承的 npm/DMG release 以 `ENABLE_LEGACY_RELEASE=true` 显式启用，Go 版本与验证工作流对齐。

### 🧠 Design Intent (Why)
减少 fork 的重复 CI 准备工作，保持三端验证覆盖和真实失败结果，并使继承的发布行为显式可控。

### 📁 Files Modified
- `.github/workflows/sdk-check.yml`
- `.github/workflows/release.yml`
- `packages/sdk/package.json`
- `packages/sdk/README.md`
- `docs/CICD.md`
- `docs/releases/RELEASE_GUIDE.md`

### Validation
- actionlint 1.7.12、文档骨架、Action SHA 和 diff whitespace 检查通过。
- SDK 类型检查与构建通过；新的 `test:built` 入口共 36 项，33 通过、2 失败、1 跳过，tarball 消费测试通过。失败仍为此前 Windows EOF / broken-input 两项断连用例，不在本次修复范围。
- 三平台 hosted runner 和发布开关的远端执行尚未运行。
