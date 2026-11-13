# 2026-11-13 上游能力接入复评

## 结论与状态

用户明确指出 Locked Use、虚拟显示器、后台操作、query、select_text、按需观察、截图配置及观察效率都很重要。此前“只吸收必要修复”是第一批同步范围，不应成为长期不接入这些功能的理由。

建议把这些能力纳入 Cherry 的接入路线，保留 SDK、应用会话、CDP、Linux Go/Wayland 引擎和发布身份。区分“功能值得做”与“上游实现能原样合并”：多数能力可以复用上游引擎，SDK 契约和资源清理必须适配。

本轮完成源码、协议和上游验证记录审查；以下接入方案尚未实施，也未执行锁屏、创建显示器或安装系统组件。上游文档中的实机结果是上游证据，不是 Cherry 的验收结果。

## 复核基线

- 本地：`cb192a10745a347f5d4754b7443367caf2196eaf`，包含上一轮必要修复。
- Cherry 远端主线：`2d9bd955201050b7020c3175839c9d30a5ad16d8`，已新增应用图标 API；本地尚未合入这部分。
- upstream 主线：`bef6c05ff0838971a99befe52fafd9ef5d54abd1`，比上一轮 `1263960` 新增了 15 个提交。
- 两个远端均已 fetch。公开 PR/issue 状态于本轮通过 GitHub API 核对。
- 实施时先整合 Cherry 最新主线和已有修复，再移植功能，避免覆盖新应用图标接口。

## 各项判断

“适配规模”描述需要改动的层次，不是工期承诺。

| 能力 | Cherry 已有基础与真实缺口 | 建议 / 适配规模 |
| --- | --- | --- |
| `query` | SDK 没有定向查询；当前必须先观察再从结果中找元素。 | 优先接入。复用有界 AX 查询，但结果纳入 `appSessionId` / `snapshotId` / `elementId` 归属；中。 |
| `select_text` | SDK 动作没有文本选择；`typeText` 替换当前选区不等于能够主动定位并选择指定文字。 | 优先接入为新的类型化动作，保留实际选区验证；中。 |
| 按需观察 | 三端 SDK 已支持 `includeScreenshot: false`，并真正跳过截图；已有 `perform()` 可执行后不自动观察。缺的是只截图、不遍历 AX，以及 CLI/MCP 对齐。 | 补齐缺失模式，不重复建立动作回读开关；中。 |
| 截图配置 | SDK 截图 MIME 固定为 `image/png`；没有公开的格式、质量与尺寸配置。 | 接入格式与尺寸能力；同时改协议、编码、消费者和打包，配置由 SDK/宿主明确传入；中。 |
| JSON 权限诊断 | SDK 已有 `getPermissionStatus()`、`getCapabilities()` 与权限申请；缺少上游 `doctor --json` 的 CLI 入口。 | CLI 复用现有诊断，SDK 接 Locked Use 时增加对应权限/安装状态；CLI 小。 |
| 控件树预算 / AX 批量读取 | 已有节点、深度、文字预算；缺少优先保留可见操作控件和单次观察内的批量读取。 | 优先接入共享引擎。Linux 应移植算法到 Go，而非恢复上游 Python；中。 |
| 软件光标跟随窗口 | 已有软件光标，但缺少上游完整的窗口移动跟踪。 | 接入跟踪算法，并明确应用停止时释放跟踪与定时器；小至中。 |
| 后台与跨 Space 操作 | CDP 已解决部分 Chromium/Electron 页面的输入；当前 macOS 观察仍依赖既有窗口匹配，缺少上游遮挡可见性维护、跨 Space 捕获与 `sky_key`。 | 核心能力，建议接入；保持 CDP 通道选择和失败影响语义；大。 |
| 虚拟显示器 | 当前没有窗口停放/恢复能力；可解决首次观察时页面已经隐藏的情况。 | 作为后台能力的显式可选扩展接入，先补多屏排列及异常恢复；大。 |
| Locked Use | 当前没有 Broker、Guardian、认证插件或受保护解锁租约。 | 纳入交付计划，以独立系统组件批次实现和验收；很大。 |

## 直接影响接入方式的发现

### 1. SDK 已有一部分“新功能”

以 Cherry 远端主线的 [协议](https://github.com/CherryHQ/cherry-computer-use/blob/2d9bd955201050b7020c3175839c9d30a5ad16d8/protocol/schema.json)和 [客户端](https://github.com/CherryHQ/cherry-computer-use/blob/2d9bd955201050b7020c3175839c9d30a5ad16d8/packages/sdk/src/computer-use.ts)为准：

- `ObserveInput.includeScreenshot`、`perform()` 和结构化权限查询均已存在。
- `ObserveInput` 没有 `includeAX`；`Action` 没有 `selectText`；客户端没有 `query`。
- `Screenshot.mimeType` 是 `image/png` 常量，不能只替换原生编码后就返回 JPEG/WebP，否则客户端校验会拒绝。

三端采集路径也已核对：macOS 的 `SnapshotBuilder`、Windows `Observe-SDK`、Linux `linuxDesktop.Observe` 都在关闭截图时跳过采集，不只是丢弃已经生成的图片。

新增只截图模式必须同时处理窗口身份、旧元素引用失效和 `tree` 未请求状态；不能用空树假装已完成 AX 观察。窗口定位可能仍需少量原生元数据，验收重点是未执行控件树、焦点和选区遍历。SDK 配置应由宿主/调用选项传入，避免任务行为受上游全局用户配置文件悄悄改变。

上游 WebP 实现增加了固定版本的 `libwebp-Xcode` 依赖；接入要同时检查 Cherry macOS 制品和第三方声明。PNG/JPEG/WebP 需通过 SDK 校验和宿主真实图片出口验证，不能只测编码器。

### 2. query / select_text 的算法可复用，引用归属需要适配

上游 `query` 在独立 registry 中保存高位数字索引，默认寿命 120 秒、容量 5000；Cherry 的所有动作绑定应用会话和快照。应让查询结果进入同一套归属及停止清理机制，避免增加一条可绕过 `stopAppSession()` 的索引路径。

复用有界搜索、角色/文字匹配、部分结果标志、进程重启及窗口身份检查。遇到预算不足时，“没找到”不能当成“目标不存在”。`select_text` 复用 UTF-16 选区、前后文消歧和写后确认，失败不能自动拖选或重复输入。

源证据：[query 语义](https://github.com/iFurySt/open-computer-use/blob/bef6c05ff0838971a99befe52fafd9ef5d54abd1/docs/targeted-query.zh-CN.md)、[选区执行与验证](https://github.com/iFurySt/open-computer-use/blob/bef6c05ff0838971a99befe52fafd9ef5d54abd1/packages/OpenComputerUseKit/Sources/OpenComputerUseKit/AccessibilityTextSelection.swift)。两项目前是 macOS 实现；Windows/Linux 应明确报告不支持，不能因 TS 方法存在就宣称三端支持。

### 3. CDP 不替代后台观察，后台观察也不自动解决首次遮挡

上游在已可见窗口上关闭 occlusion 通知，使 Chromium/Electron 后续被遮挡时仍保留页面渲染和 AX 内容；另有整窗捕获、精确 AX/CG window ID 绑定和进程定向键盘路径。精确窗口绑定也支撑 focused sheet，宜一并接入，防止树、截图和输入落在不同窗口。

如果首次接触时页面已经 hidden，仅关闭通知不能把它重新变成 visible；虚拟屏停放是上游对此的解决方式。因此后台操作与虚拟屏有互补价值，但不需要把虚拟屏设为默认。

Cherry 的 SDK 键盘路径会直接使用 `WebCDPInput` / `InputSimulation`，不会自动经过上游 CLI 的 `key_method` 路由。新增路径必须接到 SDK 原生执行层，并保留“可能已执行时不换通道重发”的约束。采集触发可见性状态维护或移动窗口时，也不能继续把它作为没有副作用的普通只读请求。

源证据：[上游后台行为和实机边界](https://github.com/iFurySt/open-computer-use/blob/bef6c05ff0838971a99befe52fafd9ef5d54abd1/docs/references/macos-window-visibility-and-spaces.md)、[Cherry SDK 原生路由](https://github.com/CherryHQ/cherry-computer-use/blob/2d9bd955201050b7020c3175839c9d30a5ad16d8/packages/OpenComputerUseKit/Sources/OpenComputerUseKit/SDKDesktop.swift)。

### 4. 虚拟屏并非完全不影响用户桌面

上游已验证创建、停放、操作、恢复的受控流程，但当前实现仍有明确副作用：新增显示器可能改变物理屏排列，鼠标可能进入不可见屏。窗口恢复依赖进程内记录、连接结束和 `atexit`；这不能保证被强杀后的恢复。

[PR #114](https://github.com/iFurySt/open-computer-use/pull/114) 正在处理显示器排列以及上次进程退出未恢复的窗口被下一次虚拟屏重新吸走的问题，本轮核对仍为 OPEN。它有单屏和残留窗口复现证据，多物理屏排列尚未实机复现。不能把这个 PR 的行为当成已合入主线。

Cherry 接入时应将停放记录归属到具体应用会话，并将显示器资源归属到控制任务。停止 A 应用不能恢复 B 的窗口；任务关闭需恢复全部所属窗口。恢复失败应保留恢复依据并反馈清理未确认，不能直接清空记录。多屏、显示器断开、窗口关闭、任务取消和 helper 崩溃应成为交付验收项。

### 5. Locked Use 值得做，但不是安装一个遮罩窗口

它包含 root Broker、Guardian 与 watchdog、认证插件、租约、输入接管、重锁呈现和清理确认。上游已有真实受保护 GUI 操作闭环证据，因此不是只有设计文档；其安装与 release 打包也已存在。

但本轮确认仍有具体缺口：

- [issue #142](https://github.com/iFurySt/open-computer-use/issues/142) 仍开放；Karabiner 独占 HID 的修复已入主线，但上游明确保留 Karabiner / Secure Input 下完整接管的待验证项。
- 新合并的 [PR #187](https://github.com/iFurySt/open-computer-use/pull/187) 修复暗屏唤醒及保活。上游测过真实电源断言和状态机回放，尚未验证签名组件的“暗屏 → 解锁 → 操作 → 重锁”完整流程。
- Broker 的 `idle` 不等于两个保护进程已释放；资源清理需要等待 `guardsReleased` 等独立确认，不能仅凭 RPC 返回或子进程退出报成功。

上游入口绑定 MCP/app-agent 连接，`LockedUseActionScope` 依赖串行派发和进程内校验 scope。Cherry 应让宿主授权并持有控制任务租约，SDK 应用会话共用该任务的保护状态；所有原生 GUI 路径，包括直接 CDP 输入，都必须经过租约与取消检查。停止、断连、本地接管和任务结束需要统一收敛，不能只把上游外层 MCP wrapper 搬过来。

系统安装路径、Broker/Guardian/插件标识、签名校验、公证和升级卸载必须使用 Cherry 自己的身份，避免与用户已有 OCU 安装冲突。普通工具调用不能隐式安装或开启 Locked Use；宿主明确启用后再使用该能力。

### 6. 效率改进要连同后续正确性修复吸收

控件树预算改进优先保留可见操作控件，避免大列表挤掉“打开”“取消”；批量 AX 读取只缓存当前观察，动作和后续观察仍读新值。这两项可落在 SDK 与 CLI 共用引擎，收益覆盖日常操作。

上游 11 月 9 日后新增了密码值隐藏、旧元素身份复核、AX 点击结果不确定时停止兜底、部分输入失败后释放按键/按钮等修复。移植查询和批量读取时应使用含这些修复的当前版本，尤其不能把密码值放进预取缓存或 query 结果。

Cherry 已有快照归属与部分身份检查，不代表新增保护全部重复。应按共享引擎和 SDK 直接路径核对；当前仍未移植这些新增提交。来源包括 [#164](https://github.com/iFurySt/open-computer-use/pull/164)、[#178](https://github.com/iFurySt/open-computer-use/pull/178)、[#183](https://github.com/iFurySt/open-computer-use/pull/183)、[#181](https://github.com/iFurySt/open-computer-use/pull/181)、[#173](https://github.com/iFurySt/open-computer-use/pull/173)。

## 建议交付顺序与验收

1. **整合基线与新增必要修复**：先保留 Cherry 新主线及 `cb192a1` 的工作，核对并吸收新增密码保护、目标身份及输入收尾修复。验证 SDK/CLI 两条调用路径，动作结果不确定时不重发。
2. **查询和观察能力**：先补精确窗口/sheet 绑定，再接 `query`、`select_text`、纯截图、可见节点预算、AX 批量读取和光标跟踪；JSON 诊断与截图选项可在这一批分小提交交付。验证真实控件、Unicode/重复文本、旧引用拒绝、未请求通道不执行采集，保留现有 CDP 和图片返回契约。
3. **后台操作与可选虚拟屏**：接入遮挡维护、跨 Space 捕获和定向键盘，再接窗口停放。验证首次已遮挡、先可见后遮挡、其他 Space、多窗口/同名窗口、真实前台输入不受误投，以及恢复失败与取消。
4. **Locked Use**：以 Cherry 独立安装/签名组件接入任务租约、接管与清理。用签名发行制品验收多屏、暗屏连续两轮、键盘/鼠标接管、Karabiner/Secure Input、Broker/Guardian 异常、卸载恢复与普通人工解锁。前面几个批次不必等待它全部完成。

上述顺序用于控制依赖和独立验收，不降低 Locked Use 的产品重要性。JS REPL 与 DeepSeek Harness 不在用户本轮点名的范围内，也不是这些能力进入 SDK 的前提。

## 证据边界

- 本轮重新检查了最新源码、SDK schema、上游测试实现和公开 PR/issue 状态。
- 未重新运行上一轮 Swift/SDK/Go 测试，因为运行时代码没有改变；上一轮通过结果只证明原有修复。
- 上游有真实 AppKit、GTK、WPF 和部分锁屏/后台操作记录，但不能代替 Cherry 包、SDK 及宿主接入后的验收。
- 本轮只对评估文档执行文档与差异检查，结果记录到同任务 history。
