## [2026-09-21 10:40] | Task: 修复 Windows SDK 断管测试

### 🤖 Execution Context
* **Agent ID**: `/root`
* **Base Model**: GPT-6
* **Runtime**: Codex desktop

### 📥 User Query
> fix ci

### 🛠 Changes Overview
**Scope:** SDK 测试夹具与 CI 文档。

- EOF、写入失败用例使用子进程额外继承的真实管道，由夹具 Socket 管理关闭。
- 输入管道关闭事件完成后才通知父进程发起失败写入。
- 保留原有错误码、effect、清理及进程退出断言；正常 SDK 与原生协议仍使用标准输入/输出。

### 🧠 Design Intent (Why)
Windows CI 的两个用例原本未能通过关闭 Node 标准流制造所需的 OS 断管，最终等待到请求超时。用独立管道明确管理故障注入的生命周期，无需修改生产 SDK 或放宽断言。

### 📁 Files Modified
- `packages/sdk/tests/client.test.ts`
- `packages/sdk/tests/fixtures/runtime.mjs`
- `docs/CICD.md`

### Validation
- Windows 的 EOF、写入失败、阻塞写三个聚焦用例通过。
- SDK 类型检查、构建、夹具语法、文档骨架、Action SHA 和 diff 检查通过。
- 完整 Windows SDK 测试：35 通过、0 失败、1 个原有平台跳过；三平台 CI 在推送后验证。
