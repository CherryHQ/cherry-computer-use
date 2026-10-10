# Linux SDK 与 CLI/MCP 共用原生引擎

## 目标与状态

把现有 Go AT-SPI/X11 后端发展为 SDK 与 CLI/MCP 共用的 Linux 引擎，逐步迁移 Python bridge 的必要能力。完成条件是两个入口调用同一份原生实现，保留各自协议和授权边界，并在能力验收后删除旧 Python 执行路径。

状态：里程碑 1–3 已实现，里程碑 4 的代码部分完成（2026-10-09）。`internal/desktop` 引擎由 SDK 与 CLI/MCP 共用，X11 全局输入改为 Go XTEST 并由任务私有 guard 清理，Python 执行路径已删除。剩余：x64 与平台 tarball 实际启动验收、语义方向滚动、Wayland。

## 背景与证据

- [Linux main.go](../../../apps/OpenComputerUseLinux/main.go)：SDK `serve` 使用 `linuxDesktop`，CLI/MCP 则通过 `runPython` 启动嵌入的脚本；后者每次重启 Python，使用独立的 30 秒 context。
- [SDK 后端](../../../apps/OpenComputerUseLinux/sdk.go)已经直接通过 `godbus/dbus/v5` 调用 AT-SPI；[截图](../../../apps/OpenComputerUseLinux/internal/desktop/capture.go)通过 `jezek/xgb` 使用 X11。
- 原 Python bridge（`runtime.py`，里程碑 4 已删除）依赖 Python、PyGObject、AT-SPI typelib，截图还使用 GDK。它提供七种动作，但部分动作使用全局键鼠，执行后直接采集快照。
- [无 Python 实验](../../../experiments/LinuxNativeProbe/README.md)已验证 Go 路径的 GTK 观察、点击和 PNG。该结果不代表其他六种动作、Wayland 或真实应用都已验收。
- [runtime 设计](../../design-docs/computer-use-runtime.md)已经把 Python 定位为过渡选项；Go 原生接入优先，但不承诺所有后端都能无 cgo 实现。
- [共享 runtime-go](../../../packages/runtime-go/README.md)已有可选 `ActionDriver` 和七种动作的参数验证，可直接供 Linux 适配，不必另建动作协议。

## 方案比较

| 方案 | 可以复用什么 | 成本与问题 | 建议 |
| --- | --- | --- | --- |
| SDK 直接接入 Python | 现有观察、动作及渲染代码 | 重新引入 Python/GI 运行依赖；仍需拆动作/观察、接入取消、快照所有权、输入门控和 worker 清理 | 仅在明确接受这些部署成本且需要短期过渡时采用 |
| 只给 SDK 补一套 Go 动作 | 现有 SDK 会话和 Go 原生连接 | CLI/MCP 继续 Python，长期维护两套窗口选择、树和动作行为 | 不推荐作为最终结构 |
| Go 共用引擎，逐步迁移两个入口 | 已有 Go D-Bus/X11 与 Python 的语义设计、工具契约 | 初期需要能力迁移和双入口验收；后续修复只发生在同一引擎 | 推荐 |

这里的复用最终是原生引擎代码复用，不是永久保留 Python 源码，也不是让 CLI 通过 SDK stdio 绕一圈。保留上游归属和许可证；迁移的是有效语义，不固化旧实现的错误行为。

## 范围与分层

- 首批覆盖 Linux AT-SPI/X11 的发现、窗口定位、树、截图和语义动作。Wayland 下可用的 AT-SPI 能力继续独立报告，截图与全局输入另行验证。
- 拟将现有 Linux 原生实现提取至 `apps/OpenComputerUseLinux/internal/desktop/`；最初只提取两个入口实际需要的接口，不预建跨平台后端框架。
- Linux 引擎负责 D-Bus/X11 连接、目标/窗口/元素原生引用、树、截图及动作执行回执。原生引用不传给 JS 后重建。
- SDK 适配负责映射现有 `DesktopDriver` / `ActionDriver`；`runtime-go` 继续持有协议 ID、应用会话、快照有效性和结果编排，避免引擎再建一套 SDK 会话缓存。
- CLI/MCP service 直接调用同一个引擎，把结果转成现有九个工具的文本/图片；保留 CLI 参数、MCP schema、树预算、文本限制与连续调用的元素索引契约。
- 每个 runtime 拥有独立引擎实例。代码共用不意味着跨任务共享 daemon、连接或可变快照。
- 不把多窗口公共 API、GPU 截图修复、Cherry 宿主全局输入授权改动混入首批迁移。窗口枚举和原生身份应留在引擎内，为后续公共窗口选择接口提供基础。

## 能力迁移约束

| 能力 | 迁移方式 | 必须验证的边界 |
| --- | --- | --- |
| 应用与观察 | 共用 AT-SPI 枚举、状态、文本/值、边界和树记录 | 稳定进程/总线身份；窗口 ACTIVE/SHOWING 优先；树预算；原生完整身份不使用截断展示文本 |
| 截图 | 保留现有 Go X11 窗口匹配与截图路径 | 歧义、最小化、像素格式、缩放和不可用状态；不改回 Python 的桌面区域裁剪，也不承诺现有 GetImage 已解决遮挡/GPU 问题 |
| `click` | 首选 AT-SPI Action | 重新验证目标、窗口、元素和动作；不隐式回退全局点击；坐标/多击/非左键等变体分别报告支持情况 |
| `performSecondaryAction` | 查询并匹配真实动作名称/描述 | 陈旧动作索引、重名与不可用动作；只执行一次 |
| `setValue` | EditableText 或 Value | 文本与数值接口区分，数值范围、只读和失败返回；写入后的真实值 |
| `typeText` | 目标窗口内具有明确焦点的 EditableText，按光标/选区处理 | 不照搬“找到第一个输入框并追加”；两个输入框、中文/emoji 字符偏移、选区、多行；目标不明确时拒绝 |
| `scroll` | 先验证控件的真实方向动作/滚动条能力；全局翻页归入单独阶段 | `Component.ScrollTo` 是把元素滚入视野，不等于按方向翻 N 页；不能把两者当作等价实现 |
| `pressKey` / `drag` | 现有 Python 依赖的全局输入另做可行性验证 | 默认禁用；不得为了凑齐七种方法宣称后台执行；键盘布局、按住状态和取消释放是启用前条件 |

能力需同时考虑会话授权、桌面后端和具体目标接口。声明支持一个动作不代表所有控件和参数变体都可执行；不支持的路径必须在产生副作用前返回明确错误。

## 不可退化的行为

1. `allowGlobalInput: false` 时不移动物理指针、不注入全局按键，不自动抢焦点。SDK 策略不能被 CLI 环境变量覆盖。
2. 全局输入即便显式允许，也要检查目标前台身份；“允许全局输入”不自动等于“允许激活应用”。焦点检查与输入之间存在竞争，不能据此承诺后台隔离，宿主仍需独占协调。
3. 原生动作与后续观察分开。动作确认完成后，截图失败/取消不改写为动作失败；派发后结果不明保留 `effect: possible`，禁止自动重试。
4. 应用会话停止、任务关闭和 EOF 都必须使旧引用失效。对可能按住的键鼠，只有确认释放后才能报告清理完成；单纯取消 context 或杀进程不算输入已释放。
5. 现有 `linuxRuntimeEnvironment` 的桌面会话环境恢复也属于迁移范围：CLI 不能失去它，SDK 不得因提取而全局修改进程环境或连接到其他用户会话。

## 里程碑与验收

### 0. 固定契约与验证难点

- 建立 Python 行为清单，区分必须保留的工具契约与明确修正的行为：文本目标、窗口选择、截图来源、全局回退和动作回执。
- 从 AT-SPI 官方 D-Bus 定义核对 Action、EditableText、Text、Value 的方法签名、返回值及字符计数；优先沿用现有库，不复制 GI 绑定层。
- 用 GTK fixture 验证焦点、选区、数值、次级动作与方向滚动。测试必须断言真实控件变化，不能只断言调用了某个 D-Bus 方法。
- 单独验证键名/布局映射和注入后的清理路径。若无现成 Go 接口足以满足要求，记录具体系统库/绑定成本再决定；不为坚持纯 Go 手写完整键盘布局或输入法。
- 验收：产出按动作/参数划分的迁移清单；不能安全支持的路径有明确阻断条件，不能用空实现填绿。

### 1. 提取引擎，接通双入口观察

- 提取已有连接、发现、窗口、树和截图代码；SDK 使用薄适配。
- CLI/MCP 的发现和观察改用同一引擎。过渡期尚未迁移的动作允许继续明确走旧 Python 路径，但必须保留所需记录字段、重新验证目标，不能在失败后自动切换后端重试。
- 保留入口各自的输出格式；同一真实窗口应得到一致的原生身份、元素语义和可用性。
- 验收：无 Python 容器中两个入口都能观察；现有 SDK 点击、过期快照、跨会话拒绝、停止、PNG 测试继续通过。仍依赖 Python 的过渡动作不计入无 Python 完成度。

### 2. 迁移语义动作

- 先完成 `click`、次级动作、`setValue`、`typeText`，两个入口同时使用同一实现；通过 `ActionDriver` 接入 SDK。
- 增加明确支持的语义滚动，仅在方向/范围含义能够成立且效果可验证时开放；否则如实拒绝。
- 验收：真实 GTK 输入和数值变化、Unicode/选区、两个输入框的正确目标、次级动作、过期/禁用元素、不明确结果不重试。默认策略下前台与物理指针不发生改变。

### 3. 补全剩余 X11 动作与清理

- 在显式允许条件下迁移全局按键、拖拽、坐标点击及翻页路径，保留 CLI 既有授权门控并映射 SDK 策略。
- 按住输入必须由可清理的资源所有者管理；先验证中途取消、断连和 owner 异常退出，再选择同进程执行或任务私有执行进程。共享引擎不预先限定进程数量。
- 验收：真实修饰键/拖拽效果、焦点切换拒绝、取消/EOF/进程异常退出后的输入释放，以及多个任务不会替对方释放输入。若不能收敛，保持路径禁用，不宣称七种动作全面迁移完成。
- 这一阶段不能让 Cherry 当前禁用的全局输入自动可用；宿主是否开放属于独立产品决策。

### 4. 删除 Python 路径并交付

- 仅当仍承诺支持的 CLI/MCP 能力都完成迁移并验证后，删除 `runPython`、嵌入脚本和仅供它使用的数据转换/依赖说明。不能用静默减少 CLI 能力换取删除 Python；主动收缩能力需单独确认。
- SDK 与 CLI/MCP 都通过无 Python 的真实桌面测试；两个入口共享引擎，取消和关闭不残留连接、进程或按住输入。
- 验证 Linux x64/arm64 构建和实际平台 tarball 启动；跨架构构建通过不能代替各架构执行证据。
- 同步架构、SDK 能力表、Linux 部署依赖、发布说明与 changeset；发布后的 Cherry 集成另验，不能由 GTK fixture 代替。
- 每个里程碑可独立形成 PR；Linux 工作不自动加入现有 Windows 修复 PR。

### 独立后续：Wayland

架构与分阶段（W0–W5）见 [Linux 显示后端架构](../../design-docs/linux-display-backends.md)。

AT-SPI 语义操作与 Wayland 截图/输入分开报告。后者验证 RemoteDesktop/ScreenCast portal、PipeWire、必要的 EIS 接入、显式用户授权、任务会话寿命及停止清理，在 GNOME 与 KDE 分别执行。优先复用平台库，经过实验再确定绑定和依赖，不能把 X11 注入机械翻译成 Wayland 支持。

## 验证方式与交付边界

- 文档阶段：`make check-docs`、相对链接存在性和 `git diff --check`。本次不改运行时代码，不用已有测试通过来声称迁移完成。
- 实现阶段：在相应 Go module 运行 `go test ./...`、`go vet ./...`；共享生命周期修改另跑 race 检查；涉及 SDK/协议时运行该包已定义的检查脚本。
- 复用 [Linux 原型容器](../../../experiments/LinuxNativeProbe/Dockerfile)、[原生协议测试](../../../protocol/native.test.mjs)和[桌面测试](../../../protocol/desktop.test.mjs)扩展契约测试，保留容器无 Python 的硬性断言。
- 增加真实 GTK fixture 的动作结果和独立焦点/指针/输入状态观测；多窗口、重复标题、进程重启、D-Bus 消失、动作成功但观察失败均需覆盖。
- 提案阶段只保存文档；里程碑 1、2 已改变 Linux 运行行为，见上方实现记录。现有 Windows PR 不受影响。后续按阶段记录本地验证、远端 CI、真实桌面和发行包覆盖，分别报告。

## 进度记录

- [x] 核对 Go SDK 与 Python CLI/MCP 的调用路径和无 Python 实验。
- [x] 比较直接复用、SDK 单独迁移与双入口共用三种方案。
- [x] 记录能力边界、迁移顺序、验收和 Python 删除条件。
- [x] 里程碑 0：已核对语义动作所需的 D-Bus 签名与字符/字节计数，并在隔离 X11 会话完成全局输入实验（见里程碑 3 记录）。
- [x] 提取共用引擎并接通双入口观察（CLI 文本树与 Python 输出逐字一致，仅数值 `0.0` 改为 `0`）。
- [x] 语义动作：`click`、次级动作、`setValue`、`typeText` 由两个入口共用；SDK 通过 `ActionDriver` 接入。
- [ ] 语义方向滚动：未开放，CLI `scroll` 仍走全局翻页键，SDK 报告不支持。
- [x] 剩余 X11 动作（全局按键、拖拽、坐标点击、翻页、无焦点打字）迁移与清理验收。
- [~] 删除 Python 执行路径：代码、测试与依赖说明已删除；x64 与平台 tarball 实际启动尚未验收。

## 里程碑 1、2 实现记录

- 引擎：[internal/desktop](../../../apps/OpenComputerUseLinux/internal/desktop/)。`Config.Env` 显式提供会话变量，SDK 传空（读进程环境），CLI 传 `linuxRuntimeEnvironment` 恢复结果；引擎不写进程环境。CLI 因 xgb 只读进程 `XAUTHORITY`，在自身进程中补写该变量，SDK 不这样做。
- 窗口：ACTIVE → SHOWING → 首个窗口，SDK 由此改为与 CLI 相同的选择。树：深度优先，`runtimeId` 为相对应用的子索引路径。逐节点的独立 D-Bus 读取并发发出；gnome-shell 1200 节点约 1.4s，Python（libatspi 缓存）约 1.0s。
- 动作：执行前重新读取名称/角色/窗口标题和父链；动作按索引与名称、描述同时匹配；派发后不随取消中止，结果未知为 `possible`，不重试、不改走指针。`setValue`：有 Value 接口时只接受有限数字并检查范围；文本要求 EDITABLE 且非 READ_ONLY；两者读回比对。`typeText`：只接受窗口内唯一聚焦的可编辑文本，先删选区再插入，读回确认。
- CLI 过渡路由（里程碑 2 时的状态，里程碑 3 已由引擎替代）：元素有语义动作且为单次左键时走引擎；坐标、非左键、多击、无语义动作的元素、`drag`、`press_key`、`scroll`，以及没有聚焦文本框的 `type_text`，由 Go 换算屏幕坐标、重新校验元素后交给 Python helper。helper 不再解析目标或采集快照，修饰键在 `finally` 中释放，未知修饰键在发送前拒绝。动作后由 Go 重新观察；观察失败时动作仍报告完成。`accessibility` 方法下 `click_count` 不为 1 改为明确拒绝（原先静默执行一次）。
- SDK：能力表中 `click`、`performSecondaryAction`、`setValue`、`typeText` 可用，`scroll`、`drag`、`pressKey` 为 unsupported；坐标、多击和非左键点击无论 `allowGlobalInput` 都在派发前拒绝。
- 已确认的接口细节：`Action.GetActions` 返回本地化名称（GTK 为 `Click`），需逐个 `GetName`；Chromium 实现 Action 却不在 `GetInterfaces` 中列出，因此总是读取 `NActions`；GTK 3 的 `EditableText.InsertText` 长度按 UTF-8 字节计，偏移按字符计；`READ_ONLY` 为第 43 位；窗口失去焦点时 GTK 不报告 FOCUSED，此时 `typeText` 拒绝。

### 验证

- `go vet ./...`、`go test -race ./...`（`apps/OpenComputerUseLinux`）；当时的 `python3 runtime_test.py`；`node --test protocol/native.test.mjs`。
- 真实 GNOME Wayland 桌面（ARM64）：`OPEN_COMPUTER_USE_LINUX_DESKTOP_TEST=1` 的引擎测试连续 4 次通过（点击、过期/禁用拒绝、复选框、次级动作、数值与越界、Unicode 文本、只读拒绝、选区替换与光标插入）；`protocol/desktop.test.mjs` 与新增 `protocol/linux-desktop.test.mjs` 对 GTK fixture 通过；CLI `call --calls` 的点击、数值、次级动作与只读拒绝通过；`list-apps` 与三个应用的 `snapshot` 与 Python 版本输出对比。
- 未覆盖：X11 截图（本机为 Wayland）、无 Python 容器重跑、x64、CLI 全局输入路径（避免向真实窗口发送按键）、Qt/Chromium 的文本动作。

## 里程碑 3、4 实现记录

### 实验结论（隔离 Xephyr，`testdata/x11-session.sh`）

- 纯 Go `jezek/xgb/xtest` 可完成按键、修饰键、指针移动/按键，无需 cgo。
- **X server 不会在 XTEST 客户端断开后释放已按下的键**：进程异常退出会留下卡住的键，因此必须有独立于 owner 的清理者。
- AT-SPI `GenerateKeyboardEvent` 的 `KEY_STRING` 把 `"z中é"` 打成 `"zéé"`；原 Python `send_text` 依赖它，CJK 全局打字本就不可靠。
- 原 Python `send_key` 把修饰键的 keysym 当 keycode 以 `PRESS` 发送，实测不会按下修饰键：`ctrl+a` 实际只输入 `a`。

### 实现

- [input.go](../../../apps/OpenComputerUseLinux/internal/desktop/input.go)：每个引擎一条 XTEST 连接。键盘动作每个键前检查目标 X 窗口（按 PID 与观察到的标题唯一匹配）或其后代持有输入焦点；指针动作先移动指针，再确认落点下最深窗口属于目标，否则不按键返回 `effect: none`。键位查当前映射前两级（Shift 级自动加 Shift）；缺少的字符成批临时映射到空闲 keycode，等待客户端读取事件后恢复。首个按下后的任何失败或取消报告 `possible`；返回前释放本动作仍按住的一切。
- [guard.go](../../../apps/OpenComputerUseLinux/internal/desktop/guard.go)：`open-computer-use input-guard <display>` 子进程，忽略 SIGINT/SIGTERM/SIGHUP；owner 在每次按下或临时映射**之前**登记，释放后注销；stdin EOF（owner 正常关闭或被杀）时只释放该 owner 仍登记且仍按下的输入并清除临时映射，然后退出。guard 退出后该引擎拒绝继续全局输入。整组 SIGKILL 同时杀死 owner 与 guard 时无法清理，这一残余风险记录在此。
- SDK：坐标相对观察到的 X 窗口（即截图坐标），越界在派发前拒绝；无 `allowGlobalInput` 返回 `PERMISSION_REQUIRED`；Wayland 返回 unsupported，不把 XWayland 注入当作 Wayland 支持。能力表在 X11 下报告 `scroll`/`drag`/`pressKey` 可用，无显示为 unavailable。有边界的元素在 X11 下也声明 `click`（无语义动作时需全局输入）。`scroll` 为翻页键，作用于目标窗口内的焦点控件。
- CLI：全部动作经引擎，`runtime.py`、`runtime_test.py`、嵌入与 `runPython` 删除；保留原有门控（只有 `click_method=global` 需环境变量），但新增焦点/遮挡检查。Wayland 下原生 Wayland 窗口会被拒绝（无法匹配 X 窗口），而旧路径会报告成功却没有送达。`XDG_SESSION_TYPE` 声明优先于仅存在的 Wayland socket，修正隔离 X11 会话中 CLI 不截图的问题。
- CI：Linux job 在 Xvfb 隔离会话中运行引擎 X11/真实桌面测试与两个 SDK 桌面测试（含截图）。

### 验证

- `go vet`、`go test -race`；隔离 X11 会话中 `TestX11GlobalInput` 连续 5 次通过（Ctrl+A、`Ab中é🙂` 临时映射、单/双击、滑块拖拽、焦点被占与遮挡拒绝、越界拖拽拒绝、取消后无按键与映射残留、owner SIGKILL 后 guard 释放、其他 owner 关闭不释放他人按键）。
- 隔离 X11：`protocol/desktop.test.mjs`（要求截图）、`protocol/linux-desktop.test.mjs`（权限门控与真实全局动作）、`native.test.mjs`；CLI `call --calls` 在 `PATH` 中没有 python3 时完成按键、打字、双击、拖拽、翻页与坐标点击并附截图。CI 步骤主体本地照跑通过（Xephyr 代替 Xvfb）。
- 真实 GNOME Wayland 桌面：语义动作测试与 `linux-desktop.test.mjs` 的 Wayland 分支通过；CLI 对原生 Wayland 窗口的 `press_key` 在发送前拒绝；无残留 guard 进程。全局输入从未注入真实桌面。
- 未覆盖：x64、平台 tarball、带窗口管理器的真实 X11 会话、非 GTK 应用、非 US 键盘布局。

## 决策与参考

- 2026-10-09：推荐基于现有 Go 后端迁移 Python 必要能力，并收敛 SDK 与 CLI/MCP。同日实现里程碑 1、2；保留 CLI 全局输入的既有能力，未静默收缩。随后实现里程碑 3 并删除 Python：选择进程内 XTEST 加 owner 私有 guard，而非让全局输入常驻独立执行进程，因为实验表明清理者只需在 owner 死亡时介入。Wayland 和全局输入的进程布局由验证结果决定，不预先承诺纯 Go 覆盖全部能力。
- [AT-SPI EditableText](https://gnome.pages.gitlab.gnome.org/at-spi2-core/libatspi/iface.EditableText.html)：文本操作与字符偏移。
- [AT-SPI Component.ScrollTo](https://gnome.pages.gitlab.gnome.org/at-spi2-core/libatspi/method.Component.scroll_to.html)：滚入视野的语义边界。
- [AT-SPI 键盘合成](https://gnome.pages.gitlab.gnome.org/at-spi2-core/libatspi/func.generate_keyboard_event.html)：作用于当前 UI 上下文。
- [XDG RemoteDesktop portal](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)：会话化桌面输入与权限。
