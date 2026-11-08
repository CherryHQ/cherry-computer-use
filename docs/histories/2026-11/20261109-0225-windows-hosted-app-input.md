## 2026-11-09 02:25 | Task: 修复 Windows 宿主应用（UWP/Chromium）输入与窗口身份

### 🤖 Execution Context
* **Agent ID**: `2d8ea3eb-ea48-4b9e-8114-b5aab07098b6`
* **Base Model**: `not recorded`
* **Runtime**: Cherry Studio automation REPL (Windows 11 desktop session)

### 📥 User Query
> 打开 Windows 计算器算 5+32*9。之后：分析 cherry-computer-use 的 bug 并给出修复；应该真正支持这类应用，而不是绕过，fallback 可以保留。

### 🛠 Changes Overview
**Scope:** `apps/OpenComputerUseWindows`（Windows Go runtime + 内嵌 PowerShell bridge）。

**Key Actions:**
- **[输入窗口路由]**: 新增 `Get-InputWindowHandle`，把键鼠消息投递到真正拥有输入队列的窗口：UWP（`ApplicationFrameWindow`）→ `Windows.UI.Core.CoreWindow` / `ApplicationFrameInputSinkWindow`；Chromium/Electron（`Chrome_WidgetWin*`）→ `Chrome_RenderWidgetHostHWND`；原生 Win32 保持顶层窗口。此前所有输入都发给 `ApplicationFrameHost` 的框架窗口并被丢弃。
- **[字符输入]**: 可打印字符统一走 `WM_CHAR` 到输入窗口，`+ * = / % ( )` 等符号不再依赖虚拟键表；`type_text` 在计算器这类无文本控件的应用上从"静默无效果"变为真实输入。
- **[按键语义]**: `SendInput` 补齐（x64 40 字节 `INPUT` 布局）：目标窗口已在前台时发送组合键、方向键、`Return`、`Backspace` 与真实指针点击；后台则返回 `UNSUPPORTED_CAPABILITY` 并说明替代方案，不再假成功。`Split-KeyChord` 重写，`+`、`ctrl++` 可表达。
- **[后台点击]**: 坐标点击新增 UI Automation 命中测试（`Send-SemanticClick`）：在目标自己的树里取“包含该点且面积最小”的元素，再触发它自己的 pattern。该做法与遮挡无关，宿主内容与经典应用都适用；双击对宿主内容按“语义激活”处理一次，而不是重复调用两次。真实指针路径（前台 + `SendInput`）仍是首选，posted 消息降为经典窗口的最后兜底。
- **[经典应用回归]**: posted 鼠标/滚轮改投到目标窗口内指针下的最深子窗口（`RealChildWindowFromPoint` 递归下探，遮挡无关），posted 键盘改投到目标进程的焦点窗口（`GetGUIThreadInfo`）。WinForms 等控件对 posted 鼠标消息常无反应，因此坐标点击优先走命中测试 + pattern。
- **[滚动]**: `Invoke-Scroll` 先判断当前轴是否可滚（无可滚内容视为 no-op，与真实滚轮一致），自身无 `ScrollPattern` 时向可滚祖先回退；宿主内容且无处可滚时显式报错。
- **[窗口身份]**: `EnumWindows` 枚举真实顶层窗口，UWP 按每个窗口的宿主 AUMID 匹配；校验改为 `IsWindow(hwnd)` + `GetWindowThreadProcessId(hwnd) == pid`，不再与 `Process.MainWindowHandle` 比较。`observe` / `act` / 截图 / `window.title` / `apps` 全部使用目标 hwnd；`apps` 按窗口枚举，UWP 用窗口标题作为名称；SDK `Target.Key` 带上 hwnd，计算器与设置成为两个独立 app。
- **[错误分类]**: 元素不支持某动作时返回 `UNSUPPORTED_CAPABILITY`（原先把 `setValue` 到按钮报成 `STALE_SNAPSHOT`）；`cancelEvent` 缺失不再报 `INVALID_ARGUMENT`。

### 🧠 Design Intent (Why)
`ApplicationFrameHost.exe` 是所有 UWP 应用共享的宿主进程：一个 PID 下挂着多个 `ApplicationFrameWindow`，`Process.MainWindowHandle` 只返回其中一个且会随激活跳变。它既导致"另一个窗口一激活，目标就永久 STALE"，也让输入全部发给了不处理消息的框架窗口。因此窗口必须按句柄识别，输入必须按真实输入窗口投递。后台无法投递的按键/指针语义改为显式能力错误，而不是返回成功却无效果。

### 📁 Files Modified
- `apps/OpenComputerUseWindows/runtime.ps1`
- `apps/OpenComputerUseWindows/sdk.ps1`
- `apps/OpenComputerUseWindows/sdk.go`
- `docs/ARCHITECTURE.md`
- `docs/exec-plans/active/20260422-windows-computer-use-runtime.md`
- `docs/releases/feature-release-notes.md`
- `skills/open-computer-use/SKILL.md`

### ✅ Verification (interactive Windows desktop)
- 计算器（UWP，`Settings` 仍占着 AFH 的 `MainWindowHandle`）：`observe` 成功且 `window.title` 正确；元素点击、坐标点击、双击、`performSecondaryAction`、`typeText`、`pressKey` 均生效；`5+32*9` 用纯键盘与纯元素点击得到相同结果（`333`，标准模式无运算优先级）。
- 设置（UWP）：`setValue` 写入搜索框、`ScrollViewer` 滚动、`observe` 正常。
- Win32 fixture（WinForms）：坐标点击、元素点击、`typeText`（`EM_REPLACESEL`）、`pressKey`、`setValue`、滚轮、拖拽均生效。
- Chromium：`Get-InputWindowHandle` 在 `Chrome_WidgetWin_1` 下正确解析出 `Chrome_RenderWidgetHostHWND`。
- `go build ./...` 通过。
