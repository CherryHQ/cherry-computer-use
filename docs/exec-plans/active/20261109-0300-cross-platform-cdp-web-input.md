# 跨平台 CDP 通道：Chromium / Electron 页面输入

状态：Active（v1 已在 Windows 与 Linux 落地，macOS 待接）

## 为什么放在共享层

CDP 与操作系统无关：发现端点（进程命令行 / `DevToolsActivePort`）、选页面 target、
`Input.insertText` / `Input.dispatchKeyEvent` 在 macOS、Linux、Windows 上是同一套协议。
放进某个平台的 bridge（最初的实现放在 Windows 的 `runtime.ps1`）会导致 macOS/Linux 重复实现，
并且把"网页输入"这件事绑在 PowerShell 上。因此实现位于共享 Go 层
`packages/runtime-go/webcdp`，由各平台 driver 提供 `(pid, 窗口标题)` 两个输入。

```
packages/runtime-go/webcdp/          # 跨平台：发现 + WebSocket + Input.*
  webcdp.go                          # Discover / PageTargets / SelectTarget / Input
  ws.go                              # 最小 RFC 6455 客户端（掩码、分片、ping/pong）
  input.go                           # insertText / dispatchKeyEvent + 键位映射
  chord.go                           # ctrl+shift+s、'+'、ctrl++ 解析
  cmdline_windows.go                 # NtQueryInformationProcess(60)，不依赖 WMI/PowerShell
  cmdline_linux.go                   # /proc/<pid>/cmdline
  cmdline_darwin.go                  # ps -o command=
apps/OpenComputerUseWindows/sdk_webinput.go   # driver 适配（pid + 窗口标题）
apps/OpenComputerUseLinux/sdk_webinput.go     # 同一适配，走 /proc
```

macOS 运行时是独立 Swift session（`packages/OpenComputerUseKit`）。它需要的契约与本包一致，
但当前没有 Go↔Swift 桥：要么按同一套命令补一份 Swift 实现，要么后续把本包编译成 helper。
在那之前 macOS 继续走 AX，功能不受影响。

## v1 行为

- 只在 `typeText` / `pressKey`（没有 element 目标）时尝试，且仅在应用**已经暴露** CDP 时生效：
  `--remote-debugging-port=<port>`，否则从 `--user-data-dir` 读 `DevToolsActivePort`。
- `typeText`：由运行时在页面里自己聚焦可编辑元素（`activeElement` 优先，否则第一个可见
  `input`/`textarea`/`contenteditable`）→ `Input.insertText` → 回读 `activeElement.value`，
  没有增长就判定失败回退。中文/emoji 与 `+ * =` 都直接通过。
- `pressKey`：`Input.dispatchKeyEvent`，命名键映射到 `key/code/windowsVirtualKeyCode`，
  修饰键转 CDP 位掩码（Alt=1 / Ctrl=2 / Meta=4 / Shift=8）。
- 任何失败（无端点、无匹配页面、无焦点元素、回读不符）都返回 `ErrUnavailable`/普通错误，
  driver 继续走原有平台路径，不改变非 Chromium 应用的行为。
- `OPEN_COMPUTER_USE_DISABLE_CDP=1` 全局关闭该通道。
- 运行时**不会**带调试开关重启用户应用：Chrome/Edge 136+ 拒绝默认 profile 上的调试开关，
  带 `--user-data-dir` 重启会丢登录态，必须有显式授权再做。

## 未完成 / 下一步

1. macOS：按同一契约补 Swift 侧 CDP 客户端（或把 `webcdp` 编成 helper），复用同一套测试向量。
2. 页面级坐标点击/拖拽：需要把窗口 client 坐标换算到视口 CSS 坐标（`Page.getLayoutMetrics` +
   窗口/视口差值），标定完成前继续用 UIA/AT-SPI 命中测试。
3. `observe`：`Accessibility.getFullAXTree` + `DOM.getDocument` 生成元素树（引用用
   `backendDOMNodeId`），截图用 `Page.captureScreenshot`（不受遮挡/最小化影响）。
4. 多窗口 / OOPIF：`Target.getTargets`、`Target.setAutoAttach`、`Browser.getWindowForTarget`。
5. 可重复 smoke：CI 里启动带调试开关的 Chromium + 固定页面，跑
   `typeText`/`pressKey`/断言页面值；本 PR 只有手工验证记录。
6. `set_value` 是否可以借 CDP 做到"按元素写值"（现在仍走各平台 Value/EditableText）。

## 决策记录

- 2026-11-09：CDP 实现放共享 Go 包，不放平台 bridge；平台只提供 `(pid, 窗口标题)`。
- 2026-11-09：只在应用已经暴露 CDP 时启用，不替用户决定重启（会丢现场）。
- 2026-11-09：`typeText` 写后回读页面值，写不进去就回退，不返回假成功。
- 2026-11-09：WebSocket 用标准库手写，`packages/runtime-go` 保持零第三方依赖。
