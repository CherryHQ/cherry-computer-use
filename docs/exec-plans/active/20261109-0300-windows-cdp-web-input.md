# Windows CDP 通道：Chromium / Electron 页面输入

状态：Active（v1 落地，后续按小节推进）

## 背景

Windows 上 Chromium / Electron 的页面内容既不吃 posted 窗口消息，UIA 也只给出可访问性树：
`type_text` 只有在页面里已经有“可写且已聚焦”的元素时才可能成功，`press_key` 的修饰键、
导航键基本无效。`Input.*` 系列 CDP 命令是这类内容唯一稳定的输入通道。

同一台机器上验证过的能力（Edge 156，独立 profile，`--remote-debugging-port`）：

| CDP 命令 | 结果 |
| --- | --- |
| `Accessibility.getFullAXTree` | 拿到 `RootWebArea / textbox / button` 与 `backendDOMNodeId` |
| `Input.insertText` | 文本写入页面输入框（含中文/emoji） |
| `Input.dispatchKeyEvent` | `+ = *` 等符号与 `Enter` 正常触发页面 keydown 处理 |
| `Input.dispatchMouseEvent` | 点击按钮触发 onclick |
| `Page.captureScreenshot` | 与遮挡无关的截图 |

## v1 范围（本仓库已实现）

- 传输层：`Open-CdpSession` / `Invoke-CdpMethod` / `Invoke-CdpScript`（`System.Net.WebSockets.ClientWebSocket`，
  没有引入新依赖）。
- 发现：`--remote-debugging-port=<port>`，或从 `--user-data-dir` 下的 `DevToolsActivePort` 读取端口，
  再 `GET http://127.0.0.1:<port>/json/list` 选页面 target（先按窗口标题精确匹配，再前缀匹配）。
- 门槛：只有窗口类为 `Chrome_WidgetWin*` 的进程才会走 WMI/HTTP 探测；UWP 与经典 Win32 不受影响，
  可用 `OPEN_COMPUTER_USE_WINDOWS_CDP=0` 强制关闭。
- 动作：`type_text`（`Runtime.evaluate` 先聚焦页面内可编辑元素 → `Input.insertText` → 回读校验）
  与 `press_key`（`Input.dispatchKeyEvent`，含修饰键位掩码与命名键映射）。
- 失败即回退：CDP 不可用、target 不匹配、写后回读无变化时返回 false，继续走原有 UIA 路径，
  不改变非 Chromium 应用的行为。

## 未完成 / 下一步

1. **页面级坐标点击与拖拽**：`Input.dispatchMouseEvent` 需要视口 CSS 坐标；浏览器窗口的
   client 区（含 Chromium 自绘工具栏）到视口还有偏移，需要一次可靠标定
   （`Page.getLayoutMetrics` + 窗口/视口差值探测），否则宁可继续用 UIA 命中测试。
2. **滚动**：`Input.dispatchMouseEvent { type: mouseWheel }` 在视口中心即可工作，可与 UIA
   `ScrollPattern` 组合。
3. **observe**：用 `Accessibility.getFullAXTree` + `DOM.getDocument` 生成元素树，引用使用
   `backendDOMNodeId`，比 UIA runtime id 稳定；截图改用 `Page.captureScreenshot`（不受最小化/遮挡影响）。
4. **多窗口 / OOPIF**：`Target.getTargets`、`Target.setAutoAttach`、`Browser.getWindowForTarget`
   把 target 与顶层窗口对应，避免多个浏览器窗口时按标题猜错。
5. **带开关启动**：目前只连接“已经暴露 CDP”的应用。带 `--remote-debugging-port` 启动需要独立
   `--user-data-dir`（Chrome/Edge 136+ 会忽略默认 profile 上的调试开关），代价是丢掉用户登录态，
   因此必须先有显式授权参数与回收策略，再考虑实现。
6. **测试**：需要一条可重复的 CDP smoke（启动带调试开关的 Chromium + 固定页面），
   目前只有手工验证记录。

## 决策记录

- 2026-11-09：CDP 只作为 Chromium/Electron 的**输入增强**，不替换 UIA；窗口框架按钮、UWP、
  经典 Win32 继续走 UIA，避免一次改动同时承担两套模型。
- 2026-11-09：默认只在“应用已经暴露 CDP”时启用，不主动带开关重启用户应用（会丢现场）。
- 2026-11-09：`type_text` 走 `Input.insertText` 后**回读页面值**判断是否真的写入，
  写不进去就返回 false 交给 UIA，不返回假成功。
