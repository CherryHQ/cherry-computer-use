# 跨平台 CDP 通道：Chromium / Electron 页面输入

状态：Active（v1 已在 Windows、Linux、macOS 落地）

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

macOS 运行时是独立 Swift session（`packages/OpenComputerUseKit`），没有 Go↔Swift 桥，因此按同一契约
补了一份 Swift 实现 `WebCDPInput.swift`（同样是发现 → 选 target → `Input.insertText` /
`Input.dispatchKeyEvent`，写后回读校验，失败返回 false 交给 AX）。Go 与 Swift 两侧的解析/键表
用同一组测试向量（见各自单测），避免行为漂移。

## v1 行为

- **通道在 observe 时解析，不在输入时试探**：`observe` 期间对目标窗口解析一次
  （`--remote-debugging-port=<port>`，否则 `--user-data-dir` 下的 `DevToolsActivePort` → `/json/list`
  按标题选页面），结果缓存在 app session / target 上。`typeText` / `pressKey` 只使用已解析的结果：
  有就发到页面，没有就直接走平台路径。不再"先打一段字看有没有反应"。
- 解析结果的失效是显式的：传输层失败（页面关闭、target 消失）→ 丢弃该缓存并回退平台路径；
  页面收到调用但值没变 → 返回 `ErrNotDelivered`，**不允许换通道重发**（否则会写两遍）。
- `typeText`：由运行时在页面里自己聚焦可编辑元素（`activeElement` 优先，否则第一个可见
  `input`/`textarea`/`contenteditable`）→ `Input.insertText` → 回读 `activeElement.value`，
  没有增长就判定失败回退。中文/emoji 与 `+ * =` 都直接通过。
- `pressKey`：`Input.dispatchKeyEvent`，命名键映射到 `key/code/windowsVirtualKeyCode`，
  修饰键转 CDP 位掩码（Alt=1 / Ctrl=2 / Meta=4 / Shift=8）。
- 失败语义分成两类：`ErrUnavailable`（没端点/没匹配页面/没聚焦到可编辑元素，什么都没发出去，
  可以安全回退）与 `ErrNotDelivered`（发出去但页面没接受，不能重发，向调用方报 `effect: possible`）。
- `OPEN_COMPUTER_USE_DISABLE_CDP=1` 全局关闭该通道。
- 运行时**不会**带调试开关重启用户应用：Chrome/Edge 136+ 拒绝默认 profile 上的调试开关，
  带 `--user-data-dir` 重启会丢登录态，必须有显式授权再做。

## 未完成 / 下一步

1. 页面级坐标点击/拖拽：需要把窗口 client 坐标换算到视口 CSS 坐标（`Page.getLayoutMetrics` +
   窗口/视口差值），标定完成前继续用 UIA/AT-SPI 命中测试。
2. `observe`：`Accessibility.getFullAXTree` + `DOM.getDocument` 生成元素树（引用用
   `backendDOMNodeId`），截图用 `Page.captureScreenshot`（不受遮挡/最小化影响）。
3. 多窗口 / OOPIF：`Target.getTargets`、`Target.setAutoAttach`、`Browser.getWindowForTarget`。
4. 可重复 smoke：CI 里启动带调试开关的 Chromium + 固定页面，跑
   `typeText`/`pressKey`/断言页面值；本 PR 只有手工验证记录。
5. `set_value` 是否可以借 CDP 做到"按元素写值"（现在仍走各平台 Value/EditableText）。

## 观测里暴露通道（已落地）

`protocol/schema.json` 的 `Snapshot.inputChannel` 是必填字段：

```json
{ "inputChannel": { "kind": "page", "page": "文档标题 - Profile 1 - Microsoft Edge" } }
{ "inputChannel": { "kind": "accessibility" } }
{ "inputChannel": { "kind": "accessibility", "reason": { "code": "...", "message": "..." } } }
```

- Go 侧：`sdkruntime.InputChannel` + `InputChannelDriver`，dispatcher 在构造 snapshot 时询问 driver，
  没实现的 driver 默认 `accessibility`；Windows/Linux 从缓存里已有的 `webcdp.Endpoint` 直接回答。
- Swift 侧：`AppControl.webCDP` 命中时输出 `page` 与页面标题，否则 `accessibility`。
- 这样调用方在 `observe` 之后就知道 `typeText`/`pressKey` 走哪条路，不需要靠失败再回退。

## 决策记录

- 2026-11-09：CDP 实现放共享 Go 包，不放平台 bridge；平台只提供 `(pid, 窗口标题)`。
- 2026-11-09：只在应用已经暴露 CDP 时启用，不替用户决定重启（会丢现场）。
- 2026-11-09：`typeText` 写后回读页面值确认投递，但**不再用"写不进去"来决定是否换通道**：
  投递失败分 `ErrUnavailable`（未发出，可回退）与 `ErrNotDelivered`（已发出，禁止重发）。
- 2026-11-09：通道在 observe 时解析一次并缓存，动作路径不做探测；失效时丢弃缓存再回退。
- 2026-11-09：WebSocket 用标准库手写，`packages/runtime-go` 保持零第三方依赖；
  Swift 侧用 `URLSessionWebSocketTask`，不引入第三方包。
- 2026-11-09：macOS 用 Swift 镜像同一契约（不把 Go 包塞进 .app bundle，保持签名与权限身份不变）。
