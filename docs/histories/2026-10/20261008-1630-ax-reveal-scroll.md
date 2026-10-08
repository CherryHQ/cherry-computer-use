## [2026-10-08 16:30] | Task: 后台滚动 Chromium 窗口

### 🤖 Execution Context
* **Agent ID**: `Claude Code`
* **Base Model**: `claude-opus-5-5`
* **Runtime**: `Conductor workspace, macOS`
* **Continuation**: `Codex / GPT-6` 完成位移验证修正、回归与文档；以上保留初版执行记录。

### 📥 User Query
> 后台滚动飞书没有效果（动作报告完成但界面不变），参考上游与 Cua 的实现补上后台滚动。

### 🛠 Changes Overview
**Scope:** `packages/OpenComputerUseKit`

**Key Actions:**
- **揭示滚动**: 新增 `RevealScroll`，在目标元素或其最近的 `AXScrollArea`/`AXWebArea` 内选出视口外的后代并执行 `AXScrollToVisible`，执行后重新读取坐标确认移动。
- **接入滚动链路**: `performScroll` 在 AX 翻页动作之后、定向滚轮之前尝试揭示滚动。三条路径统一检查滚动条数值或内容几何在目标方向上的变化；没有位移证据就报错，SDK 保留 `effect: possible`，不把未知结果说成没有效果。
- **单测**: 选取与位移断言覆盖四个方向、一页/两页、缺少离屏节点、过大容器、跨列排除、边缘细条、不均匀行距、超大距离和无位移。缺少候选不再推断为到边。
- **授权诊断**: 同 bundle ID 的本地 helper 与 Cherry 制品曾使用不同签名身份。显式固定为同一证书，用户重新添加屏幕录制授权后，新 SDK 会话确认辅助功能和屏幕录制均为 granted。构建证书不写进仓库。

### 🧠 Design Intent (Why)
Chromium/Electron 窗口在后台丢弃滚轮事件，上游只有 AX 翻页动作和 `postToPid` 两条路。`AXScrollToVisible` 是不发输入事件、不抢焦点的语义路径。Cua 用网页区域作为视口，而飞书的细条钳在列表边缘，所以这里先以目标元素作为容器。

### 📁 Files Modified
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/AccessibilityRevealScroll.swift`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/ComputerUseService.swift`
- `packages/OpenComputerUseKit/Tests/OpenComputerUseKitTests/RevealScrollTests.swift`
- `THIRD_PARTY_NOTICES.md`
- `docs/design-docs/computer-use-runtime.md`

### 验证与待办

- `swift test`：195 项，1 项跳过，0 失败；新增 RevealScroll 10 项覆盖目标选择和位移判定。
- `npm run sdk:check` 通过；`npm run sdk:test` 36/36；使用 release helper 的 `node --test protocol/native.test.mjs` 7/7。
- `OPEN_COMPUTER_USE_VISUAL_CURSOR=0 .build/debug/OpenComputerUseSmokeSuite`：无窗口 fixture 的 9 个工具通过。没有执行会显示光标的 `--cursor-idle-only`，此 smoke 不经过真实 AX 滚动路径。
- `bash scripts/check-docs.sh`、`git diff --check` 通过。
- 飞书一次向下滚动通过内容位移断言，随后鼠标稳定断言失败；Chrome 的前台切换和启动竞争使验收中止。无法排除人工操作影响，不能据此宣称焦点、鼠标及窗口层级稳定。
- 用户正在使用电脑，明确要求先做其他检查。飞书上下往返、消息历史、Chrome 嵌套/整页、原生应用回退与 Cherry 消息列表的严格验收留在 active plan；未推送。

### 已知限制

- 距离近似；未暴露离屏节点的虚拟列表无法靠候选缺失确定边界。
- 指定容器不会自动换成外层容器，但 AXScrollToVisible 可能连带滚动外层。
- 缺少可读滚动条或稳定后代坐标的原生控件，以及未指定具体列表的窗口级目标，可能无法验证位移。失败后重新观察，不自动重放。
