# 新增桌面能力与接入边界

本实现从 upstream `bef6c05ff0838971a99befe52fafd9ef5d54abd1` 按功能移植，保留 Cherry 协议 v2、应用会话、CDP、Linux Go/Wayland 和 Changesets 分发。虚拟屏排列与系统自动移窗恢复另取自上游 [PR #114](https://github.com/iFurySt/open-computer-use/pull/114) 的 `3dedad7f6346d4d0208873c87e4da06a45b33a31`；这是固定来源的移植，不代表该 PR 已在上游合并。未引入 JS REPL 或 DeepSeek Harness。

## 平台能力

| 能力 | macOS | Windows / Linux |
| --- | --- | --- |
| 控件树、截图、两者同时采集 | 支持 | 支持 |
| 可见控件优先的有界树读取 | AX 批量读取和缓存 | UIA 缓存 / Go AT-SPI 有界规划 |
| `query`、`selectText` | SDK 和 CLI/MCP | 明确返回不支持 |
| PNG / JPEG / WebP、尺寸与 JPEG 质量 | 支持 | 继续 PNG，拒绝 macOS 专用截图设置 |
| 目标窗口、后台键盘、虚拟显示器、Locked Use | 支持下述显式调用 | 明确返回不支持，保留原有平台/CDP 输入 |

macOS 的 CLI/MCP 现在有 11 个工具；Windows/Linux 保留原有 9 个。能力可用性通过 SDK `getCapabilities()` 查询；权限、目标应用实现和系统版本仍影响真实执行。

## SDK 调用

所有动作仍绑定 `appSessionId`、`snapshotId` 与该快照的 `elementId`。查询结果中的匹配项在 `result.snapshot.tree.elements` 中，后续动作使用 `result.snapshot.id`。新查询或观察替换旧元素引用；跨应用、旧快照及已停止的会话均拒绝操作。

```ts
const result = await computer.query({
  appSessionId: session.id, text: '发送', role: 'button', exact: true,
  limit: 20, maxNodes: 500
})
// result.truncated / stopReason / visitedNodes 区分预算耗尽与搜索完成。

const state = await computer.getAppState({
  appSessionId: session.id, includeAX: false,
  screenshot: { format: 'jpeg', maxDimension: 1280, jpegQuality: 0.8 }
})
```

`includeAX` / `includeScreenshot` 默认均为 true，不能同时为 false。未请求的通道不采集，分别返回 `OBSERVATION_NOT_REQUESTED` / `CAPTURE_NOT_REQUESTED`；只截图不会留下旧 AX 引用。返回图片使用真实 MIME 与像素尺寸，坐标输入按这些像素映射。WebP 为无损编码，`jpegQuality` 仅影响 JPEG。SDK 配置来自本次调用，不读取 CLI 用户的截图配置。

`query` 需要 `text` 或 `role`，默认最多 20 个结果、检查 500 个节点，上限分别为 100 / 5000；`stopReason` 为 `complete`、`limit`、`max_nodes`、`timeout`、`ax_error` 或 `text_limit`。查询不会截图，也不把 CLI 高位索引 registry 暴露给 SDK。

```ts
await computer.act({
  type: 'selectText', appSessionId: session.id, snapshotId: snapshot.id,
  elementId: field.id, text: '目标文字', prefix: '前文', suffix: '后文',
  selection: 'text' // 另有 cursor_before / cursor_after
})
```

文本选择按 UTF-16 选区匹配并读回确认；密码字段、歧义或不支持的文本控件拒绝执行。`perform()` 同样支持该动作，执行后不自动观察。

macOS `windowId` 可限定查询和观察的原生窗口；身份必须仍属于目标进程。默认观察不激活。`keyMethod: 'skyKey'` 显式选择窗口定向后台键盘；默认 `auto` 保留 Cherry CDP 优先路由，原生键盘回退要求快照窗口仍为应用焦点窗口。已投递或结果不确定时不切换通道重发。跨 Space、遮挡状态由原生引擎维护，效果取决于应用是否接受后台输入。

明确窗口观察与查询只接受该窗口内的焦点和选区，不会带出同应用另一文档的选中文字。窗口查找包括 `AXSheets` 中失去焦点的原生文件弹窗；停放 sheet 时仍移动其父窗口。

## 任务资源与关闭

```ts
const computer = await ComputerUse.start({
  taskOptions: { virtualDisplay: true, lockedUse: true }
})
try {
  const app = await computer.resolveApp({ query: 'TextEdit' })
  const session = await computer.openAppSession({ appId: app.id })
  await computer.getAppState({ appSessionId: session.id, windowPlacement: 'agentDisplay' })
  // windowPlacement: 'restore' 可提前恢复；默认 keep 不移动窗口。
} finally {
  await computer.close()
}
```

两项任务选项默认关闭，可单独启用。`configureTask` 是协议 v2 的可选扩展，在初始化后、打开应用前调用；不传 `taskOptions` 的现有客户端不发送该请求。旧后端无法满足显式任务配置时，启动失败并清理所属进程。

窗口停放和遮挡维护由应用控制会话持有；虚拟屏与 Locked Use 租约由任务私有 app agent 持有。停止单应用恢复它的窗口，保留其他应用及任务保护；任务关闭尝试恢复全部窗口，再确认锁屏保护释放。恢复失败保留恢复依据并返回 `CLEANUP_FAILED`，不会报告清理完成。启用任务保护时客户端给予 15 秒清理预算，原生 watchdog 为 10 秒；普通任务继续原有较短预算。

请求在申请 Locked Use 租约前就登记取消令牌，应用停止也能取消唤屏和保护准备。观察失败或取消时恢复本次新增的停放和遮挡状态；位置恢复失败不阻止尝试释放遮挡状态，失败记录保留以供关闭时重试。

首次修改窗口前，任务启动同一可执行文件的私有恢复子进程，通过继承管道登记原位置。子进程持有窗口 AX 引用及进程实例，自己设置并记住原遮挡通知状态；登记确认后才允许创建虚拟屏或移窗。拥有者正常退出、崩溃或被 SIGKILL 时，管道 EOF 触发恢复。没有全局守护进程、网络端口或持久窗口记录。身份暂时不可读时重试；已关闭或归属变化的窗口不重新猜测目标。恢复子进程也被杀死、系统重启、目标应用无响应及显示器热插拔仍可能阻止恢复。

恢复进程再启动只负责虚拟屏的子进程，创建和排列都在该进程中进行。macOS 可能在释放 `CGVirtualDisplay` 对象后仍保留重新排列过的屏幕，因此清理先恢复窗口，再结束显示器进程，并通过重新枚举确认显示器消失。不能仅检查缓存的 `CGDisplayIsOnline` 或丢弃 Swift 引用后就返回清理完成。

每次创建虚拟屏使用新的标识，避免 WindowServer 复用上次任务的停放记录。保留已有显示器原点，把虚拟屏放在最外侧右下角接触的位置，避免整条共享边和夹在用户屏幕之间；检查配置结果后才停放窗口，无法确认时销毁虚拟屏并报错。[Quartz 不允许显示器之间存在空隙](https://developer.apple.com/documentation/coregraphics/cgconfiguredisplayorigin(_:_:_:_:))，因此无法通过空隙彻底阻止鼠标进入。角落排列不等于鼠标隔离；虚拟屏会临时改变显示拓扑，不能承诺完全不影响桌面。

## CLI 配置与 Locked Use 组件

- `doctor --json` 非交互输出权限诊断；`locked-use status --json` 输出组件就绪状态。
- `get_app_state` 接受 `include_ax`、`include_screenshot`；macOS 另有 `window_id`、`window_placement: keep|agent_display|restore`，`query` / `select_text` 和键盘 `key_method: auto|sky_key`。参数以 `tools/list` 为准。
- macOS CLI 截图配置位于 `$XDG_CONFIG_HOME/cherry-computer-use/config.json`，默认 `~/.config/cherry-computer-use/config.json`。`OPEN_COMPUTER_USE_CONFIG_FILE` 可指定绝对路径；环境变量覆盖文件值。

```json
{
  "image": { "format": "webp", "maxLongEdgePixels": 1280, "jpegQuality": 0.8 },
  "virtualDisplay": { "enabled": false }
}
```

Locked Use 的内核身份校验需要 macOS 14.4 或更新版本，用户还需在 `locked-use settings` 中明确安装、启用和验证系统组件，SDK opt-in 本身不安装或申请管理员权限。Broker、Guardian、AuthorizationPlugin 与 installer 使用 `com.cherryai.cherrystudio.computer-use.locked-use.*` 身份；安装路径为 `/Library/Application Support/CherryComputerUse/LockedUse`，认证插件为 `CherryLockAuth.bundle`，不复用上游身份。物理接管、断连或任务关闭会终止租约，等待动作排空、重锁和保护释放。

HID 监控逐设备打开，允许部分设备被独占，但要求可观察设备覆盖全部输入类别。设备变化、启动期间已有输入或 Input Monitoring 被撤销时停止准备或终止租约；每次 HID 回调只检查已缓存状态，权限由健康检查轮询。Guardian 的 `--input-monitor-probe 5` 可单独诊断设备覆盖和活动计数，不创建遮罩、不锁屏、不解锁，不输出键值或设备身份。该探针不能代替 Karabiner 开启、Secure Input、完整锁屏和物理接管验收。

`build-open-computer-use-app.sh` 的 `OPEN_COMPUTER_USE_INCLUDE_LOCKED_USE=auto|0|1` 默认仅在使用 Developer ID Application 签名身份的 release 构建中捆绑四个组件。组件必须使用 Developer ID Application 证书，拒绝 ad-hoc 和 Apple Development；组件先独立签名，外层签名保留它们的 identifier 和 hardened-runtime 标志，`verify-locked-use-bundle.sh` 检查架构、签名与 Team ID。构建不会安装或启动系统服务。libwebp 依赖固定为 1.6.0，许可证随 app 和 npm 平台包分发。

## 验证边界

自动验证覆盖查询预算与引用归属、文本选择、采集通道跳过、编码、树预算、Windows 目标身份、Linux 密码字段与控件预算、Locked Use 状态机/IPC/拒绝路径、SDK 清理和打包。见[接入计划](../exec-plans/completed/20261113-upstream-feature-adoption.md)和[任务历史](../histories/2026-11/20261109-0542-upstream-reconciliation.md)。

后续真实 fixture 验证覆盖后台选区隔离、原生保存弹窗、SDK 停止恢复和拥有者 SIGKILL 后的窗口/虚拟屏恢复；Developer ID 组件完成构建、签名和离线身份校验。结果与边界见[恢复计划](../exec-plans/completed/20261113-desktop-resource-recovery.md)。

正式发布仍需分别确认：签名组件安装/升级/卸载、锁屏完整循环及物理接管、暗屏恢复、多物理显示器/跨 Space，以及 Windows/Linux 真实桌面。单测、headless fixture、签名校验与这些实机结果不能互相替代。
