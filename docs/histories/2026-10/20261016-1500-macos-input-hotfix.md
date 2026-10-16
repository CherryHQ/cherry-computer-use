## 2026-10-16 15:00 | Task: 修复 macOS AX 输入能力与点击定位

### 🤖 Execution Context
* **Agent ID**: `/root`
* **Base Model**: GPT-6
* **Runtime**: Codex

### 📥 User Query
> 修复 Computer Use：Dia 展示地址栏误报 setValue、写入前拒绝导致会话锁定、AX 点击命中位置偏移。

### 🛠 Changes Overview
**Scope:** macOS OpenComputerUseKit；SDK 使用说明。

**Key Actions:**
- 复用快照已读取的 AXValue 可写状态，仅为可写文本控件声明 setValue。
- 将 setValue 写入前的能力拒绝和 AX 查询失败转换为无副作用错误；保留写入后不确定错误的保护。
- AX 命中测试直接使用窗口坐标，不再重复应用截图缩放。
- 增加针对只读文本、合成节点、错误副作用和不同截图缩放比例的回归测试。

### 🧠 Design Intent (Why)
展示字段不等于可写输入框。能力声明应与原生检查一致，失败分类应区分输入前和输入后；坐标转换在截图入口完成一次。

### 📁 Files Modified
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/AccessibilitySnapshot.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/ComputerUseService.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/Errors.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/SDKDesktop.swift`
- `packages/OpenComputerUseKit/Tests/OpenComputerUseKitTests/SDKDesktopInputTests.swift`
- `packages/sdk/README.md`
- `.changeset/macos-input-preflight.md`

### Validation
- 恢复旧能力和坐标逻辑时，新回归测试失败；修复后 25 项定向 Swift 测试通过。
- 本地原生 Dev 应用构建、Developer ID 签名成功，辅助功能与屏幕录制预检均已授权。
- Dia 展示地址栏不再声明 setValue；强制调用返回 UNSUPPORTED_CAPABILITY / effect:none，同一会话随后可继续观察。
- Dia 在只读观察间发生外部页面变化，已停止输入，临时标签导航不计为通过。
- 独立 AppKit 测试应用：AX 元素点击计数 0→1；1280×753 截图对应 1500×882 窗口，坐标点击计数 1→2；可写字段的中文 setValue 经新 AX 观察和截图确认。观察显式使用 activation:allow。
- SDK 文档骨架检查通过；未发布版本，也未修改 Cherry Studio 的正式依赖或本地运行时链接。

### Dia follow-up acceptance
- 用户重新开启辅助功能后，新私有会话确认两项权限均 granted。
- Cmd+T → 可写 commandBarTextField 的 setValue → AX 回读 URL → Return 导航通过；本地 HTTP 请求、页面 URL、标题和截图一致。
- 网页输入框中文 setValue 通过；Cmd+L 可打开真正可写的地址栏。
- 展示地址栏点击未打开编辑态；网页计数按钮的 AX 元素点击及截图坐标点击均返回 completed，但后续计数仍为 0。这是尚未解决的 Dia 点击投递问题，不能将原生返回成功等同于页面交互成功。
- 本轮临时标签已关闭，AX 确认测试页不再存在且恢复此前页面；测试服务与私有运行时已清理。
- 三处修复仍有效，但 Dia 整体验收未通过。下一步应记录原生实际选择的点击路径，对比 AXPress 与进程/窗口定向鼠标事件；不自动重放或改用全局输入。

### PR preparation
- 同步最新 main 的 SDK 0.1.7 和稳定应用 ID 修复，保留本地 Linux/CuaDriver 探索改动且不纳入本 PR。
- 补充 SDK patch changeset；版本号和原生包由后续版本 PR 统一更新，本轮不发布。
- 同步后 26 项定向 Swift 测试通过，`make check-docs`、`git diff --check` 与 `npm run changeset:status` 通过。
- Codex 原生 Computer Use 在相同 Dia 测试页验证了 AX 点击、坐标点击和地址栏编辑态；此对照不证明本仓库的 Dia 点击投递已修复。
