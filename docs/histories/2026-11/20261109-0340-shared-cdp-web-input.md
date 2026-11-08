## 2026-11-09 03:40 | Task: 把 CDP 页面输入下沉到跨平台共享层

### 🤖 Execution Context
* **Agent ID**: `2d8ea3eb-ea48-4b9e-8114-b5aab07098b6`
* **Base Model**: `not recorded`
* **Runtime**: Cherry Studio automation REPL (Windows 11 desktop session)

### 📥 User Query
> 但是这个其他操作系统也能实现，我们是不是应该重新组织一下文件？

### 🛠 Changes Overview
**Scope:** `packages/runtime-go`（共享 Go 层）+ Windows / Linux driver + 文档。

**Key Actions:**
- **[共享包]**: 新增 `packages/runtime-go/webcdp`：端点发现、页面 target 选择、最小 RFC 6455
  WebSocket 客户端（掩码/分片/ping-pong）、`Runtime.evaluate`、`Input.insertText`、
  `Input.dispatchKeyEvent` 与键位映射。零第三方依赖，`packages/runtime-go` 保持标准库。
- **[按平台发现]**: `cmdline_windows.go` 用 `NtQueryInformationProcess(60)` 读别的进程命令行
  （不走 WMI/PowerShell）；`cmdline_linux.go` 读 `/proc/<pid>/cmdline`；`cmdline_darwin.go` 用
  `ps -o command=`。其余平台返回不可用。
- **[driver 接入]**: Windows 的 `windowsDesktop.Act` 与 Linux 的 `linuxDesktop.Act` 在
  `typeText`/`pressKey`、且没有 element 目标时先尝试 `webcdp`；成功即返回，失败继续原有
  UIA / AT-SPI / 全局输入路径。Linux 侧这一步发生在 `allowGlobalInput` 检查之前，
  因为页面输入不需要全局输入授权。
- **[关闭开关]**: `OPEN_COMPUTER_USE_DISABLE_CDP=1`（跨平台），替代原来 Windows 专属的
  `OPEN_COMPUTER_USE_WINDOWS_CDP=0`。
- **[macOS]**: 同一契约用 Swift 实现（`WebCDPInput.swift`）：`ps` 读命令行、`DevToolsActivePort`、
  `/json/list` 选 target、`URLSessionWebSocketTask` 传 `Input.insertText` / `Input.dispatchKeyEvent`，
  写后回读校验；`MacOSSDKDesktop.perform` 在非 fixture 模式下先试 CDP，失败回退 AX / `InputSimulation`。
  同步入口用受限等待把 async 传输桥接回 SDK 的同步动作路径。
- **[测试]**: `webcdp` 内新增纯 Go 单元测试：handshake accept key（RFC 示例）、帧编解码
  （含 16/64 位长度）、ping→pong、握手成功/失败、命令行与 `DevToolsActivePort` 解析、
  target 选择、键位描述符。

- **[不靠输入探测]**: 通道改为在 `observe` 时解析一次并缓存在 app session（Windows/Linux: `webcdp.Resolve`
  + 目标 key；macOS: `AppControl.webCDP`），`typeText`/`pressKey` 只用已解析结果。失败分两类：
  `ErrUnavailable`（未发出，安全回退）与 `ErrNotDelivered`（页面收到但没接受，禁止换通道重发）。

### 🧠 Design Intent (Why)
CDP 与操作系统无关，把它写在 Windows 的 PowerShell 里意味着 macOS/Linux 必须各自再实现一遍，
而且把"网页输入"绑在某个平台的 bridge 上。共享包只依赖 `(pid, 窗口标题)` 两个平台输入，
各 driver 负责提供，后续 macOS 也可以按同一契约实现或复用该包。写入后回读页面值仍然是硬要求：不确认就不报成功；但**确认只用来报告，不用来决定换通道**，
否则"输入被页面清空/替换"这类正常行为会触发二次输入。通道本身在 observe 时确定，动作路径零探测。

### 📁 Files Modified
- `packages/runtime-go/webcdp/`（新增 6 个实现文件 + 1 个测试文件）
- `apps/OpenComputerUseWindows/sdk_webinput.go`（新增）、`apps/OpenComputerUseWindows/sdk.go`
- `apps/OpenComputerUseLinux/sdk_webinput.go`（新增）、`apps/OpenComputerUseLinux/sdk.go`
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/WebCDPInput.swift`（新增）
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/SDKDesktop.swift`
- `packages/OpenComputerUseKit/Tests/OpenComputerUseKitTests/WebCDPInputTests.swift`（新增）
- `packages/runtime-go/README.md`
- `docs/exec-plans/active/20261109-0300-cross-platform-cdp-web-input.md`
- `docs/ARCHITECTURE.md`
- `docs/releases/feature-release-notes.md`
- `skills/open-computer-use/SKILL.md`

### ✅ Verification
- `go test ./...`（`packages/runtime-go`，含 webcdp）通过；`go vet` 通过（windows/linux/darwin 三目标）。
  本机 `-race` 不可用（既有包同样报 0xc0000139，环境缺 race 运行时），交给 CI。
- Windows 真实 SDK 协议（`serve --stdio`，JSON-RPC + Content-Length）驱动 Edge 156（带
  `--remote-debugging-port`）的固定页面：
  `typeText "5+32*9"` → 页面输入框 `5+32*9`；`pressKey Return` → 页面 keydown 处理器
  `enter:5+32*9`；`typeText "+*=中文"` → `5+32*9+*=中文`。
- 同一协议驱动上游 WinForms fixture（`protocol/fixtures/windows-actions.ps1`）七动作序列 11/11：
  `clicks=3 wheels=1 keys=1 drags=12`，非 Chromium 应用的路径未受影响。
- Linux：`GOOS=linux go build ./...` 通过；真实桌面验证留给 CI（4 个 Linux 场景）。
- macOS：Swift 侧只能由 CI（`SDK and native (macos-26)`：`swift test` + 打包）编译验证，本机无 Swift
  工具链；单测覆盖与 Go 相同的向量（命令行/`DevToolsActivePort` 解析、target 选择、键表、禁用开关）。
