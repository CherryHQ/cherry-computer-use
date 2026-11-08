## 2026-11-09 03:00 | Task: Windows 通过 CDP 投递 Chromium / Electron 页面输入

### 🤖 Execution Context
* **Agent ID**: `2d8ea3eb-ea48-4b9e-8114-b5aab07098b6`
* **Base Model**: `not recorded`
* **Runtime**: Cherry Studio automation REPL (Windows 11 desktop session)

### 📥 User Query
> 打开计算器算 5+32*9 → 分析 cherry-computer-use 的 bug → 应该真正支持这类应用 → Electron/Chromium 能否通过 CDP 调试的形式启动？→ 提交一个新的 PR。

### 🛠 Changes Overview
**Scope:** `apps/OpenComputerUseWindows`（Windows Go runtime 的内嵌 PowerShell bridge）+ 相关文档。

**Key Actions:**
- **[CDP 传输层]**: `Open-CdpSession` / `Invoke-CdpMethod` / `Invoke-CdpScript`，基于
  `System.Net.WebSockets.ClientWebSocket`，无新增依赖；发送/接收都带超时，事件帧会被跳过。
- **[端点发现]**: 从进程命令行解析 `--remote-debugging-port`，否则读 `--user-data-dir` 下的
  `DevToolsActivePort`，再 `GET /json/list` 选页面 target（标题精确匹配 → 前缀匹配 → 第一个 page）。
- **[门槛与回退]**: 仅当顶层窗口类为 `Chrome_WidgetWin*` 才做探测（UWP/经典 Win32 不付 WMI 成本）；
  `OPEN_COMPUTER_USE_WINDOWS_CDP=0` 可关闭；任何失败返回 false，继续走原有 UIA 路径。
- **[text]**: `type_text` 先用 `Runtime.evaluate` 聚焦页面内的可编辑元素（`activeElement` 优先，
  否则第一个可见 `input/textarea/contenteditable`），再 `Input.insertText`，最后回读
  `activeElement.value` 确认长度增加，否则判定失败并回退。
- **[key]**: `press_key` 走 `Input.dispatchKeyEvent`，命名键映射到 `key/code/windowsVirtualKeyCode`，
  修饰键转成 CDP 的位掩码（Alt=1 / Ctrl=2 / Meta=4 / Shift=8），字符键带 `text`。

### 🧠 Design Intent (Why)
Chromium/Electron 的页面内容不吃 posted 消息，UIA 只能看到可访问性树，`type_text` 依赖“页面里已有可聚焦的可写元素”，
`press_key` 的修饰键/导航键基本无效。CDP 的 `Input.*` 是这类内容唯一稳定的输入通道，但需要应用在启动时
带调试开关，因此默认策略是“只在应用已经暴露 CDP 时启用”，不主动带开关重启用户应用（会丢登录态）。
写入后回读页面值，是为了避免再出现“返回成功但页面没变”的老问题。

### 📁 Files Modified
- `apps/OpenComputerUseWindows/runtime.ps1`
- `docs/exec-plans/active/20261109-0300-windows-cdp-web-input.md`
- `docs/ARCHITECTURE.md`
- `docs/releases/feature-release-notes.md`
- `skills/open-computer-use/SKILL.md`

### ✅ Verification (interactive Windows desktop)
- Edge 156 以 `--remote-debugging-port=9333 --user-data-dir=<临时目录>` 打开固定测试页：
  - `type_text "5+32*9"` → 页面 `input.value = "5+32*9"`，且是运行时自己聚焦的输入框（无需前台、无需用户先点）
  - `press_key Return` → 页面 keydown 处理生效（`log = "enter:5+32*9"`）
  - `press_key Backspace` → `value = "5+32*"`
  - `type_text "+*=中文"` → `value` 追加 `+*=中文`（符号与 CJK 都通过）
- 回归：上游 `protocol/fixtures/windows-actions.ps1` 七动作序列本地复跑 10/10
  （`clicks=3 wheels=1 keys=1 drags=12`）；UWP battery（Calculator/Settings）13/13。
- 未覆盖：页面级坐标点击/拖拽、CDP 版 observe 与截图（记录在 exec plan 的下一步）。
