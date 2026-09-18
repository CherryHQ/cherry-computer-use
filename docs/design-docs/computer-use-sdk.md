# packages/sdk 设计草案

状态：客户端、协议 schema、三端原生生命周期及首个桌面切片已实现。Windows 11 ARM64 和 Linux X11 已通过 SDK 真实观察、截图、语义点击测试；macOS 生命周期通过，GUI 验收等待系统授权。其余动作、平台包和 npm 发布尚未完成。实际 API 见 [SDK README](../../packages/sdk/README.md)；本文继续维护 [SDK 与 npm 执行计划](../exec-plans/active/20260916-sdk-npm-distribution.md)的原生改动与后续阶段边界。

## 目标与边界

发布 `@cherrystudio/computer-use`，供 Node / Electron 主进程通过类型化 API 控制本地桌面。第一阶段由 Cherry 原有的普通工具入口调用 SDK，不依赖 Code Mode。持久脚本入口作为后续可选增强；用户关闭 Code Mode 时继续走原方案。macOS、Windows、Linux 使用相同的 SDK；平台能力如实报告，平台实现分别验收。

| 层 | 持有的状态与职责 |
| --- | --- |
| Cherry 主进程服务 | 用户授权、Agent 任务归属、目标范围、多个控制任务的协调、产品状态展示 |
| Agent 宿主的 JS 执行环境（后续阶段） | 启用 Code Mode 时持有跨脚本变量与函数、代码执行与中断、文本/图片输出；通过绑定 API 访问桌面能力 |
| `packages/sdk` | 本客户端的进程与连接、请求取消、协议握手、结果校验、错误映射 |
| Swift / Go runtime | 当前控制连接的快照、原生元素引用、输入状态、OS 权限、截图、overlay 与资源清理 |

第一版一个 `ComputerUse` 实例对应一个控制任务使用的私有 runtime 连接与所属 helper 生命周期。宿主在一个任务内复用实例，不按每次 tool call 重启；任务结束或取消后关闭。macOS 使用私有 app-agent 实例与 socket，Windows/Linux 使用所属子进程。先不做连接池、跨任务重连恢复或公共常驻 daemon。

一个控制任务可以包含多次普通工具调用。SDK 实例与原生快照在调用之间存活，不需要 JS 变量跨调用保留。后续引入持久脚本时，JS 上下文另由宿主管理，不能把 SDK 生命周期绑定到单段脚本。

这个控制任务不保存聊天记录、模型上下文或 Agent 历史。Cherry 在整个“观察 → 操作 → 再观察”的任务期间协调桌面控制；只把单次 click 排队，不能避免两个 Agent 的步骤交错。本 SDK 不承诺阻止其他独立应用控制同一桌面。

## 每应用控制会话：原生契约已实现，宿主接入待完成

2026-09-18 决定在任务级 SDK/runtime 会话内增加每应用控制上下文，供普通工具和后续 Code Mode 共用。完整资源归属、停止顺序、光标边界及验收条件见 [runtime 设计](computer-use-runtime.md)。SDK 与三端原生契约现已实现；Cherry 的控制任务归属、用户停止状态、Tray 和光标仍待接入。下面的公共 API 已更新为协议 v2。

- Cherry 持有应用控制归属、用户停止状态和 Tray 展示；SDK/runtime 持有可定位、可停止的应用上下文，不为每个应用强制创建新进程。
- 每应用上下文隔离窗口、快照、原生引用、动作/输入状态与软件光标；关闭应用上下文不退出用户的目标应用，也不自动关闭其他应用上下文。
- SDK 已提供 `openAppSession`、`listAppSessions` 和 `stopAppSession`；观察/动作必须携带 `appSessionId`，快照返回相同归属。请求级 `AbortSignal` 与任务级 `close()` 保留；v1 helper 在握手时被拒绝。显式重开已停止目标会创建新 ID，是否允许重开仍由宿主决定。
- 用户在 Tray 停止某应用或全部控制后，原生端必须拒绝新操作、取消队列并确认资源释放；宿主阻止 Agent 通过后续调用或重新创建实例自动恢复控制。重新允许由用户决定。
- 已发生动作保留实际影响状态；光标动画不作为成功凭据。每应用光标不能消除全局输入争用，跨任务/跨应用的协调仍由 Cherry 负责。

## 目录

```text
packages/sdk/
  package.json
  README.md
  tsconfig.json
  src/
    index.ts           # 仅导出公共入口与类型
    computer-use.ts    # 公共 API 与实例状态
    runtime.ts         # 平台包解析、启动与退出
    connection.ts      # RPC 库适配、取消与协议握手
    types.ts           # 公共调用选项与结果类型
    errors.ts          # 稳定错误码与动作影响状态
    generated/         # 从协议 schema 生成的 wire 类型与校验器
  tests/
    lifecycle.test.ts
    actions.test.ts
    package.test.ts
    fixtures/          # 可控子进程；不是 GUI 成功的证明
```

协议 schema 与跨语言 fixture 放在仓库根 `protocol/`。第一版只发布一个 SDK npm 入口；transport、schema 和平台解析模块不作为子路径公共 API。`packages/sdk` 不包含 Swift/Go 源码、平台二进制、Electron 依赖或 MCP 安装脚本。Agent 使用说明与示例随 SDK 的 README/类型声明分发，保持与真实 API 同源。

## 入口无关的消费契约

```text
普通工具调用 ────────────────────┐
                               → Cherry 主进程服务 → SDK → 原生 runtime
后续可选 Code Mode 的脚本调用 ────┘
```

第一阶段沿用普通工具的调用方式和授权入口，把执行实现接到 SDK。SDK 不区分调用来自工具还是脚本，不接收模型对象、聊天历史或 Code Mode 开关；工具定义、授权交互、文本和图片的模型返回格式由 Cherry adapter 负责。

- 宿主在控制任务开始时创建一个实例，并在观察、点击、输入等多次调用间复用。SDK 不新增 Agent session registry，也不要求 Agent 先调用一个管理进程的工具。
- 普通工具可以分别暴露 `click`、`scroll`、`type_text`，在 adapter 中映射到类型化的 `act(Action)`；SDK 无需再维护一份工具注册表或重复的快捷方法。
- 每次调用都由宿主绑定目标范围与取消信号。工具参数校验通过不等于已获用户授权；后续脚本入口也经过同一调用边界。
- 请求失败按 SDK 返回的错误码与动作影响状态处理。更换调用入口不能成为自动重试理由；Code Mode 的开关和执行失败都不触发动作重放。

### 能力、OS 权限与宿主授权

这三件事分别回答不同的问题，不能折叠成一个 `available: boolean`：

| 来源 | 回答的问题 | 使用方式 |
| --- | --- | --- |
| `getCapabilities()` | 当前 runtime / 桌面后端实现了哪些观察与动作能力，哪些依赖或会话条件尚不满足 | 报告能力和不可用原因；安装平台包或列出一个方法不代表该能力已可用 |
| `getPermissionStatus()` | 当前 runtime 身份是否具备所需 OS 权限 | 查询不弹窗；未授权与平台未实现分别表达，不把所有平台套进 macOS 的权限项目 |
| Cherry 的授权与目标范围 | 用户是否允许这次任务执行这个操作 | 由宿主决定，SDK 的能力查询和 OS granted 都不能代替它 |

查询结果用于展示和调用前判断；权限、目标与桌面状态可能随后变化，原生执行入口仍需验证并返回真实失败原因。第一阶段先覆盖实际遇到的能力和权限项，不建立插件式 capability registry。

## 公共 API

以下是公共调用面的摘要。完整数据结构以 [协议 schema](../../protocol/schema.json)和 SDK 导出的声明为准，JS 专用类型在 SDK 转换层组合。

```ts
interface StartOptions {
  runtimePath?: string
}

interface CallOptions {
  signal?: AbortSignal
  timeoutMs?: number
}

interface ComputerUseClient {
  getCapabilities(options?: CallOptions): Promise<Capabilities>
  getPermissionStatus(options?: CallOptions): Promise<PermissionStatus>
  requestPermissions(input: PermissionRequest, options?: CallOptions): Promise<PermissionStatus>
  listApps(options?: CallOptions): Promise<AppInfo[]>
  openAppSession(input: OpenAppSessionInput, options?: CallOptions): Promise<AppSession>
  listAppSessions(options?: CallOptions): Promise<AppSession[]>
  stopAppSession(input: AppSessionInput, options?: CallOptions): Promise<StopAppSessionResult>
  getAppState(input: ObserveInput, options?: CallOptions): Promise<Snapshot>
  act(input: Action, options?: CallOptions): Promise<ActionResult>
  onClosed(listener: (event: ClosedEvent) => void): () => void
  close(): Promise<void>
}

// ComputerUse.start(options?, callOptions?) 返回 Promise<ComputerUseClient>
```

使用形状如下；`selectedApp` 和 `selectedElement` 由宿主根据应用列表与本次快照选择：

```ts
import { ComputerUse } from '@cherrystudio/computer-use'

const computer = await ComputerUse.start({ runtimePath }, { signal })
try {
  const session = await computer.openAppSession({ appId: selectedApp.id }, { signal })
  const snapshot = await computer.getAppState(
    { appSessionId: session.id, activation: 'never' },
    { signal }
  )
  const result = await computer.act(
    { type: 'click', appSessionId: session.id, snapshotId: snapshot.id, elementId: selectedElement.id },
    { signal }
  )
  // 宿主使用 result.observation 获取后续快照或显示采集失败原因。
} finally {
  await computer.close()
}
```

- `start()` 显式启动并完成版本握手；握手失败需要收回本次已启动资源。导入模块不会启动 helper 或打开权限窗口。
- `runtimePath` 省略时解析已安装的平台包；显式提供时，macOS 指向完整 `.app`，Windows/Linux 指向原生可执行文件。SDK 内部转换为实际启动命令，不把此差异传到动作 API。
- `getPermissionStatus()` 仅查询；`requestPermissions()` 是用户操作后显式发起的 OS 权限流程。macOS 复用原生拖拽引导，调用等待拖拽被接受、用户完成或关闭窗口；拖拽被接受不等于实际获授权；默认 5 分钟超时，支持取消，结束后宿主关闭会话并用新 helper 查询。它不替代 Cherry 的业务授权，也不承诺打开系统设置后立即得到 granted。
- `getAppState()` 默认 `activation: 'never'`；允许恢复隐藏窗口或激活目标时由宿主显式传 `allow`。平台无法遵守该约束时返回结构化不可用原因，不能悄悄激活应用。
- `onClosed()` 仅提供连接最终关闭通知，返回取消订阅函数。第一版不加入通用事件总线、模型流或虚构的动作进度百分比。

### 动作面

`Action` 用 `type` 区分输入类型，承接现有七类动作；无需另建 `tools.call(name, unknown)` 公共入口。

| SDK action | 对应现有能力 | 主要输入 |
| --- | --- | --- |
| `click` | `click` | snapshot、element 或截图坐标、按键与次数 |
| `performSecondaryAction` | `perform_secondary_action` | snapshot、element、该元素声明的动作 ID |
| `scroll` | `scroll` | snapshot、可选 element、方向与数量 |
| `drag` | `drag` | snapshot、截图坐标中的起终点 |
| `typeText` | `type_text` | snapshot、文本；作用于所绑定目标内的焦点 |
| `pressKey` | `press_key` | snapshot、规范化按键组合 |
| `setValue` | `set_value` | snapshot、element、值 |

元素点击与坐标点击是互斥的输入变体，避免同时传 `elementId` 和 `x/y`。平台特定的 click backend 名称留在原生实现中；公共 API 表达语义操作或坐标输入意图。会影响系统指针/前台的路径必须由宿主明确允许，支持该约束的后端才能执行；`auto` 不能绕过限制。对第三方应用自身造成的焦点变化，只能报告实际结果，不能宣称完全可控。

## Snapshot 与动作结果

Snapshot 至少包含 `id`、`appSessionId`、应用和窗口信息、结构化元素、截图及采集状态。应用/窗口/元素 ID 均由 runtime 生成，作用域绑定本实例；Swift AX 对象、Windows HWND/UIA runtime ID、Linux AT-SPI 路径保留在原生端。

首版先收敛以下数据契约，不直接导出某个平台的内部结构：

| 数据 | SDK 消费契约 |
| --- | --- |
| 应用与窗口 | `listApps()` 返回不透明的应用 `id`，作为 `openAppSession` 的 `appId`；观察和动作使用返回的应用会话 ID；名称等展示信息与身份分开。快照记录实际观察的窗口，不能把同名应用或重启后的进程当成旧目标 |
| 元素 | 返回可筛选的结构化 ID、角色、名称、值及可用动作；树层级需要结构化关系，不能要求调用者解析缩进文本。原生引用留在 runtime |
| 采集状态 | 区分“没有元素”和“无权读取/采集失败”；节点、深度或文本预算造成的截断需要显式标记，不能让 Agent 把不完整结果当成完整桌面状态 |
| 截图 | 图片字节与 MIME、尺寸一起返回；截图不可用时保留具体原因，与元素树是否可用分别表达 |

原生采集时产生这些字段，SDK 做校验与字节转换；普通工具 adapter 再渲染为模型能读的文本和图片，后续脚本可直接筛选结构化数据。SDK 不返回某个模型框架的 `ToolCallResult`，也不负责提示词、token 预算或历史压缩。

- `snapshotId` 绑定应用进程实例、窗口和元素映射，不能把旧的 `element_index=3` 用到另一张快照中。
- 重新观察同一目标、执行可能改变目标状态的动作、目标退出或 runtime 重启，均使旧快照失效。动作执行时还需验证目标和原生引用；这不能消除外部应用在验证后变化的全部竞态。
- 坐标输入统一使用实际返回截图的像素空间，原点为左上角。缩放与窗口/屏幕转换由 runtime 保存并执行；SDK 不另建坐标缓存。没有有效截图的快照不能用于坐标动作，但可保留可用的语义动作。
- 图片在 wire 中使用 base64，在 SDK 中解码为 `Uint8Array`，同时返回 MIME、宽高。首版不增加图像管道、磁盘截图缓存或第二次自动缩放。

动作与后续观察分开表达：

```ts
interface ActionResult {
  status: 'completed'
  observation:
    | { status: 'available'; snapshot: Snapshot }
    | { status: 'unavailable'; reason: ObservationFailure }
}
```

`completed` 表示原生动作步骤已完成，不表示“提交表单”等业务目标必然达成。原生端确认点击已执行但截图失败时，返回 `completed + observation.unavailable`，宿主可以重新观察，不能据此自动重试点击。

动作失败、取消、超时或连接中断则返回稳定错误码与 `effect: 'none' | 'possible' | 'applied'`，表示已确认未执行、可能发生部分影响、或已确认执行。可参考的错误码为 `TARGET_UNAVAILABLE`、`STALE_SNAPSHOT`、`PERMISSION_REQUIRED`、`UNSUPPORTED_CAPABILITY`、`CANCELLED`、`TIMEOUT`、`RUNTIME_EXITED`、`PROTOCOL_MISMATCH`、`RUNTIME_NOT_FOUND`；首版随真实失败路径定义，禁止从人类错误文案反推代码。

## 原生协议与取消

三端已增加独立的 `serve --stdio` SDK 入口，JSON-RPC 2.0 仅作为传输协议。已接通生命周期、`listApps`、`getAppState` 和单次左键元素语义 `click`；macOS 显式权限申请已接通；Windows/Linux 的 `requestPermissions`、坐标/多次/非左键点击及其余六种动作暂报 unsupported。现有 MCP `tools/list` 与 `tools/call` 仍由 MCP adapter 提供。

JS 端已采用 [vscode-jsonrpc](https://github.com/microsoft/vscode-languageserver-node/blob/main/jsonrpc/README.md)的流读写与 Content-Length framing，并使用 `json-rpc-2.0` 管理请求关联；取消通知与所属进程生命周期由 SDK 绑定。子进程写入失败测试暴露了 `vscode-jsonrpc` 9.0.2 connection wrapper 的未处理 rejection，因此没有使用该 wrapper，也没有添加全局异常吞错。选择依据与完整 wire 契约见 [协议说明](../../protocol/README.md)。Swift/Go SDK 入口使用相同 framing；现有 MCP 的逐行 JSON 协议保持兼容。

JSON Schema 是 wire 数据的单一来源，TS wire 类型与校验器在构建时生成；可用 [Ajv standalone](https://ajv.js.org/standalone.html)生成运行时校验代码。公共 `Uint8Array`、`AbortSignal` 等 Node 调用类型只在 SDK 转换层出现。原生请求也必须验证，不能把 TS 类型当成接收边界校验。

控制链路必须满足以下规则：

1. 输入读取循环与动作执行分离；动作串行执行，取消/关闭消息继续被读取。macOS proxy 到 app agent 的中间一跳也必须支持此模式，不能继续同步“一问一答”。
2. `AbortSignal` 转成对应 request ID 的取消通知；native 确认终态后，原请求才结束。排队动作收到取消后不得开始。
3. 超时发送取消，但超时本身不是执行端已停止的证据。超过有界清理期限仍不能确认停止时，实例进入不可用状态，停止接受后续动作并关闭所属 runtime；错误保留 `effect: possible` 和清理状态。
4. macOS 中断本次操作并释放键鼠与 overlay；Windows 将取消 context 传给所属 UIA worker；Linux SDK 在 Go 内持有 D-Bus/X11 资源。发送信号或结束 Node Promise 都不能替代平台资源回收。
5. `close()` 幂等，先停止接收新请求、取消未结束请求，再关闭属于本实例的 helper 并等待退出。清理无法确认时报告失败，不能伪装成功；已发生的动作不回滚。
6. 异常 EOF、宿主退出和 bootstrap 失败同样清理；macOS 需要 owner connection 断开后的 app-agent 退出路径。只终止本实例拥有的进程，不复用上游“未知 socket 对端先 terminate”的行为。
7. 不自动重连重放动作。实例关闭后旧 snapshot 一律失效，由宿主明确重新开始。

## 现有代码必须修改的地方

基于 `386a260d1ab8b690adbbb27f7471595cf0c2b752` 的源码检查：

| 当前实现 | SDK 所需改动 |
| --- | --- |
| [ComputerUseService.swift](../../packages/OpenComputerUseKit/Sources/OpenComputerUseKit/ComputerUseService.swift)公开方法返回 `ToolCallResult`，内部缓存最近快照 | 提取结构化领域结果与快照身份，MCP adapter 再格式化 |
| [AccessibilitySnapshot.swift](../../packages/OpenComputerUseKit/Sources/OpenComputerUseKit/AccessibilitySnapshot.swift)保留内部元素引用和文本渲染 | 输出可序列化元素 DTO，原生引用保持私有，明确截图坐标映射 |
| [MCPServer.swift](../../packages/OpenComputerUseKit/Sources/OpenComputerUseKit/MCPServer.swift)同步读取/执行，turn-ended 只清 overlay | SDK 入口增加非阻塞的控制消息处理；不把 turn-ended 当成取消或退出 |
| [MacOSAppAgentProxy.swift](../../apps/OpenComputerUse/Sources/OpenComputerUse/MacOSAppAgentProxy.swift)代理与 socket 请求同步，真实 agent 通过 LaunchServices 启动 | 增加所属实例握手、异步消息转发、断连清理；操作参数通过请求传递，不借全局环境变量切换 |
| [Windows main.go](../../apps/OpenComputerUseWindows/main.go)与 [Linux main.go](../../apps/OpenComputerUseLinux/main.go)缓存 snapshot，再把结果转为 MCP 内容；bridge 使用独立超时 context | 复用结构化数据，传递请求取消，分离动作执行与后续采集结果 |

生命周期与首个桌面切片已独立接通：SDK backend 直接读取平台 API，Windows 复用 bridge 定义，结果不经过旧 CLI/MCP 展示文本。上表保留完整迁移目标，当前尚未把旧 CLI/MCP 全部改成领域核心的 adapter。

## 包与验证

- 首版以 Cherry 使用的 Node 24 为基线，并测试 Cherry 实际 Electron 主进程；不先承诺浏览器、renderer 或所有历史 Node 版本。
- 提供编译后的 ESM/CJS、类型声明与单一根 `exports`，无模块级可变 singleton；内部不依赖消费者的 TypeScript 编译配置。
- `files` 只含分发代码、声明、README 与许可证。平台二进制通过配套平台包解析，SDK 启动时不联网安装；Electron 中真实资源路径由 Cherry 提供。
- 连接测试使用真实可控子进程，覆盖分帧、请求错误、动作期间取消、EOF 和 close；平台 fixture 证明真实操作与资源释放。二者的证明范围分开报告。
- 最小回归必须能捕捉：旧快照引用点错目标、取消后排队动作继续运行、动作后截图失败导致重复执行、macOS proxy 已退出但所属 agent 泄漏、tarball 缺少入口/类型/runtime 资源。

## 实现顺序与验收

三端进程关系、控制通道与具体改动位置见 [原生运行时设计](computer-use-runtime.md)。

1. SDK、`protocol/` 与 Swift/Go 生命周期切片已落地；`start → initialize → capabilities → close` 已在三端本机环境验证。真实键鼠输入取消恢复属于尚未接入的动作阶段。
2. 三端已接入 `listApps → getAppState → click` 结构化链路，覆盖快照失效和动作/采集结果区分。Windows ARM64/Linux X11 真实 GUI 通过；macOS GUI 待授权。其余六类动作仍不支持。
3. 已采用固定证书重建 macOS helper，重新授权及权限保持/真实桌面验收仍待完成。每应用控制上下文与 v2 协议契约已实现；下一步接 Cherry 控制归属、用户停止状态、Tray 单应用/全部停止和已支持点击的软件光标。
4. 通过 Cherry 原有普通工具入口复用任务级 SDK 实例及应用控制上下文，完成“观察并返回图片 → 下一次工具调用执行动作”，验证用户停止后不能被 Agent 自动恢复；关闭 Code Mode 也必须完整可用。
5. 扩展输入、移动、拖拽及分步取消，补齐七类动作与平台限制，使用打包后的 SDK/runtime tarball 完成三端桌面验收。持久脚本、JS reset 与跨脚本变量留到后续 Code Mode 阶段单独验收。

各步分别验收，不把进程 fixture 通过、交叉编译通过或 npm 安装通过当成三端功能交付完成。

## 后续决策：可选 Code Mode 与持久脚本

2026-09-16 确认：这部分不属于第一阶段，不作为 SDK、npm 分发或三端接入的验收前提。后续沿用 Cherry 现有 `meta` 工具入口与共用 `codeMode` 执行核心演进，不在 computer-use 包内另建 REPL；Browser Use 与 Computer Use 可通过同一执行能力接入。

Code Mode 关闭时，Agent 继续调用原有的应用查询、观察和动作工具，由 Cherry 主进程服务调用同一 SDK。开启时才增加脚本入口；SDK 不读取这个用户设置。脚本失败后不能自动改走普通工具重放动作，因为原生操作可能已经发生。

这里的“Codex 风格”指持久 JavaScript 上下文、异步 SDK 调用、显式输出截图、跨调用复用变量的使用体验，不声称复刻 Codex 的内部实现。

```text
Agent 编写 JavaScript
  → 宿主 execute 工具 / 已有原生 REPL
  → 持久 JS 上下文中的 computer API
  → Cherry 主进程的调用边界
  → packages/sdk
  → macOS / Windows / Linux runtime
```

Agent 可以使用循环、条件、函数和结构化数据筛选。默认由宿主提供已绑定的 `computer`，脚本不需要自行管理二进制路径、启动或关闭 helper。宿主自有代码或独立 Node 脚本仍可正常 `import { ComputerUse }` 并管理完整生命周期。

### 两段脚本示例

以下是拟议接口。`appId` 从前一步 `computer.listApps()` 的返回结果中选择，`output` 由脚本执行环境提供，不属于底层 SDK。

```js
// 第一段：采集并把需要的内容交给模型。
var snapshot = await computer.getAppState({ appId })
output.text(snapshot.tree)
if (snapshot.screenshot.status === 'available') await output.image(snapshot.screenshot.image)
```

模型看到元素与图片后，选择本次快照中的 `buttonId`，编写下一段：

```js
var result = await computer.act({
  type: 'click',
  snapshotId: snapshot.id,
  elementId: buttonId
})
if (result.observation.status === 'available') {
  snapshot = result.observation.snapshot
  output.text(snapshot.tree)
  if (snapshot.screenshot.status === 'available') await output.image(snapshot.screenshot.image)
} else {
  output.text(result.observation)
}
```

`snapshot` 能在两次调用间保留，但其原生有效性仍由 runtime 检查。使用 `var` 展示可重复执行的交互式绑定；具体 REPL 的顶层绑定、await 和重声明行为必须测试，不能只复用一个包着 async function 的 Worker 就宣称支持持久变量。

图片输出完成并返回模型后，模型才能据此决定下一步；`output.image()` 不会让正在执行的 JS 自动理解图像。已确定目标与验证条件的步骤可以在一段脚本中组合，需要新的视觉判断时结束本段并返回观察结果。持久脚本初版不让后台 timer 或未归属当前调用的异步任务继续发出桌面操作。

### SDK 与宿主的接入约定

- `computer` 复用 SDK 的方法名、参数与返回类型，持久脚本初版暴露能力查询、应用列表、观察和动作。`start/close`、任意 runtime 路径和 OS 授权流程由宿主管理，不向 Agent 注入完整 SDK 管理对象。
- 每次桥接调用绑定真实的任务/脚本身份和取消信号，并检查当前允许的目标和操作。脚本中的循环不会绕过这条调用边界；调用参数里的授权标志不视作宿主授权。
- `output.text` 和 `output.image` 由执行环境收集为工具返回的文本/图片块。图片不能仅作为 base64 字符串写进 JSON 文本；必须经过各 harness 的真实多模态返回路径验证。
- 同一上下文一次执行一段脚本。普通脚本结束前收敛该段已发起的请求；结束后拒绝该段遗留异步回调的桌面调用。用户停止则取消原生请求并结束控制任务，不能只终止 JS Worker。
- JS reset 后旧变量、SDK 连接与 snapshot 不再可用。Agent 对话和上下文仍由原 harness 管理，不序列化或恢复原生元素句柄，也不重放历史脚本。
- 有原生持久 JS 环境的 harness 通过其受支持的扩展方式绑定 API；其他 harness 使用 Cherry 共用执行能力。`packages/sdk` 本身不承担解释器或 Agent 调度职责。

### Cherry 当前具备的基础与缺口

以 Cherry Studio `d8ac9372e909a4103fbb35b3f243c5cd30e03ac1` 的本地源码为依据：

| 当前入口 | 已有能力 | 所需改动 |
| --- | --- | --- |
| `src/main/ai/tools/adapters/aiSdk/meta/` | 搜索、检查、调用工具及 `toolExec` 定义 | 复用现有 Code Mode 入口；`applyDeferExposition.ts` 当前没有注入 `tool_exec`，不能把定义存在当成默认启用 |
| `src/main/ai/tools/codeMode/runtime.ts` | `runExecCode`、Worker、超时、子调用取消 | 当前每次创建/终止 Worker；增加任务内持久上下文及显式 reset/stop 生命周期 |
| `src/main/ai/tools/codeMode/worker.ts` | async wrapper、`tools.invoke`、日志与请求收敛 | wrapper 局部变量不跨脚本保留；增加明确的 REPL 绑定语义和 `computer` 注入 |
| `src/main/ai/runtime/pi/piCodeMode.ts` | Pi 工具适配、嵌套授权及等待授权时暂停计时 | 当前 `toPiResult` 返回文本 JSON；增加真实图片输出，维持逐次操作授权 |
| `src/main/ai/tools/adapters/aiSdk/meta/exec/runtime.ts` | 共用执行器的 AI SDK adapter | 当前拒绝需交互授权的嵌套工具；需在该授权入口完善可暂停/恢复行为，不能在 computer-use SDK 内绕过 |

这是共用脚本执行器需要补齐的能力，实施前应围绕该 owner 落宿主侧改动，避免在 computer-use 包内另写一个重复执行器。当前 Worker 注释明确说明它不是安全沙箱；仅换成 `node:vm` 也不能形成安全边界，[Node 官方文档](https://nodejs.org/api/vm.html#vm-executing-javascript)明确说明这一限制。允许执行何种代码及提供何种进程/系统隔离由 Agent 宿主负责，SDK 通过注入对象限制 API 面不等于限制了任意 Node 代码权限。

现有源码只能证明上述 Pi / AI SDK 路径，不代表所有 harness 已具备持久 REPL 或图片返回支持；每条实际接入路径分别验收。
