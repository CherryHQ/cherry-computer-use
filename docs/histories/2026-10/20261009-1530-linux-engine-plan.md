## [2026-10-09 15:30] | Task: 规划 Linux 共用引擎迁移

### Execution Context

- Agent ID: `/root`
- Base Model: GPT-6
- Runtime: Codex / Conductor

### User Query

> 开始计划 Linux 如何复用上游 runtime，或将 Python 实现迁移。

### Changes Overview

Scope: Linux SDK / CLI/MCP 架构计划，仅文档。

- 比较直接复用 Python、SDK 单独迁移和双入口共用 Go 引擎。
- 推荐从现有 Go AT-SPI/X11 后端提取共用引擎，分阶段迁移 Python 的必要能力。
- 明确语义动作、全局输入、Wayland 的不同边界；记录双入口验收和 Python 删除条件。

### Design Intent

保留已有无 Python 部署成果，同时消除 SDK 与 CLI/MCP 两套原生实现的长期维护成本。推荐方案仍需原生接口与输入清理实验，不把规划当作已实现能力。

### Files Modified

- [执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)

### Validation

- 文档骨架、所增文档相对链接及空白检查。
- 未修改运行时代码；Linux 能力迁移尚未实施。

## [2026-10-09 15:57] | Task: 实现 Linux 共用引擎里程碑 1、2

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 继续实现 PR（按计划完成里程碑 1 + 2）。

### Changes Overview

Scope: `apps/OpenComputerUseLinux`、协议测试与相关文档。

- 新增 `internal/desktop` Go AT-SPI/X11 引擎，SDK 与 CLI/MCP 共用发现、窗口选择、树、截图和语义动作；X11 截图从 SDK 文件移入引擎。
- SDK 改为薄适配并实现 `ActionDriver`：语义点击、次级动作、`setValue`、`typeText`；全局输入动作在派发前拒绝。
- CLI/MCP 的 `list_apps`、`get_app_state` 和语义动作改用引擎，文本树格式与元素索引保持不变；`runtime.py` 缩减为只发送全局键鼠输入的 helper，修饰键在异常时也会释放。
- 新增 GTK fixture、可选真实桌面引擎测试与 `protocol/linux-desktop.test.mjs`；更新原生协议测试中的 Linux 能力断言。

### Design Intent

按计划先让两个入口调用同一份原生实现，同时不静默减少 CLI 能力：尚未迁移的全局输入保留旧路径，但由 Go 在派发前决定路线和坐标，避免失败后自动切换后端。语义动作统一执行前校验、单次派发与读回确认，修正旧实现“追加到第一个输入框”和 Unicode 长度按字符计的问题。

### Files Modified

- [Linux 引擎](../../../apps/OpenComputerUseLinux/internal/desktop/)
- [SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[CLI 适配](../../../apps/OpenComputerUseLinux/cli_engine.go)、全局输入 helper `runtime.py`（后续已删除）
- [Linux 动作测试](../../../protocol/linux-desktop.test.mjs)、[原生协议测试](../../../protocol/native.test.mjs)
- [执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)、架构、SDK/协议 README 与 changeset

### Validation

- Go vet、race 单测、Python helper 单测、原生协议测试通过。
- 真实 GNOME Wayland 桌面的引擎、SDK 与 CLI 测试通过，CLI 输出与 Python 版本对比一致；X11 截图、无 Python 容器、x64 与 CLI 全局输入路径未在本轮重跑。

## [2026-10-09 16:15] | Task: 实现 Linux 全局输入迁移并删除 Python

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 实现下一步（里程碑 3，并在验证后完成里程碑 4 的代码部分）。

### Changes Overview

Scope: `apps/OpenComputerUseLinux`、协议测试、CI 与相关文档。

- 在隔离 Xephyr 会话中验证全局输入方案：XTEST 可用；X server 不释放断开客户端按住的键；原 Python 修饰键与 CJK 打字路径有缺陷。
- 新增引擎 XTEST 输入（按键、打字、指针点击、拖拽、翻页），键盘要求目标窗口持有焦点、指针要求落点属于目标窗口；缺失字符临时映射后恢复。
- 新增 `input-guard` 子进程：按下前登记，owner 任意方式退出时只释放其仍按住的输入。
- SDK 在 X11 且 `allowGlobalInput: true` 时开放这些动作，Wayland 报告 unsupported；CLI 全部改走引擎，删除 `runtime.py`、其测试与嵌入。
- 新增 `testdata/x11-session.sh` 隔离会话，CI Linux job 在 Xvfb 中运行全局输入与 SDK 桌面测试。

### Design Intent

按计划先实验再定进程布局。实验证明 XTEST 按键在客户端死亡后会卡住，所以清理者必须独立于 owner；guard 只做清理，执行仍在进程内，避免为每个动作跨进程通信。焦点与遮挡检查让输入不会落到其他窗口，Wayland 下拒绝而不是假装送达。

### Files Modified

- [输入与 guard](../../../apps/OpenComputerUseLinux/internal/desktop/input.go)、[guard](../../../apps/OpenComputerUseLinux/internal/desktop/guard.go)、[键位](../../../apps/OpenComputerUseLinux/internal/desktop/keys.go)
- [SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[CLI 适配](../../../apps/OpenComputerUseLinux/cli_engine.go)、[隔离会话脚本](../../../apps/OpenComputerUseLinux/testdata/x11-session.sh)
- [CI](../../../.github/workflows/sdk-check.yml)、[执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)、架构、SDK/协议 README 与 changeset

### Validation

- 隔离 X11 中引擎全局输入测试、SDK 两个桌面测试与 CLI 无 Python 运行通过；CI 步骤主体本地照跑通过。
- 真实 Wayland 桌面只验证了拒绝路径与语义动作，未向其注入全局输入。x64、平台 tarball、带窗口管理器的 X11 与非 GTK 应用未验证。

## [2026-10-09 16:45] | Task: 规划 Linux X11/Wayland 显示后端架构

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 决定自研；从架构上支持 X11 与 Wayland，并考虑不同合成器和桌面环境，先给出架构概览。

### Changes Overview

Scope: 设计文档，仅文档。

- 新增 Linux 显示后端架构：引擎保持 AT-SPI 部分不变，截图、窗口定位、输入与授权拆为按会话能力组合的 provider。
- 给出 X11、XWayland、wlroots、GNOME、KDE 的后端矩阵，探测顺序、坐标模型、输入安全契约、门户授权映射、依赖与测试策略，以及 W0–W5 分阶段和待确认问题。

### Design Intent

Wayland 下各合成器提供的协议不同，不能写成单一后端；按能力拼装并逐项报告可用性，才能在不支持的组合上如实拒绝。门户坐标只对显示器流给出位置，所以指针输入被列为需要实验确认的部分，键盘和语义动作先行。

### Files Modified

- [Linux 显示后端架构](../../design-docs/linux-display-backends.md)
- [设计文档索引](../../design-docs/index.md)、[执行计划](../../exec-plans/active/20261009-linux-shared-engine.md)

### Validation

- 文档检查与相对链接检查；门户接口细节已对照官方文档核对，未经实验的结论在文中标注为待确认。

## [2026-10-09 17:00] | Task: 确定 Linux Wayland 后端的语言分工

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> Wayland 的具体细节不适合用 Go 实现，按"Go 保留现有部分、Wayland 用 Rust 辅助进程"修改架构。

### Changes Overview

Scope: 设计文档，仅文档。

- 架构文档新增"语言与进程边界"：Go 保留协议、会话、AT-SPI、X11 与 `input-guard`；Wayland 协议、门户、libei、PipeWire 与合成器 IPC 由按需启动的 Rust 辅助进程实现。
- 定义辅助进程的启动时机、stdio 私有协议、所有权与 EOF 清理、系统库处理、构建分发与供应链要求；更新依赖表、测试策略、W0/W1 与待确认问题。

### Design Intent

Go 缺少成熟的 Wayland、libei、PipeWire 实现，cgo 会让 X11 用户也依赖这些库并破坏交叉编译。独立进程沿用 Windows PowerShell 引擎与 `input-guard` 的模式，同时把会话清理和崩溃隔离放在进程边界上。

### Files Modified

- [Linux 显示后端架构](../../design-docs/linux-display-backends.md)、[设计文档索引](../../design-docs/index.md)

### Validation

- 文档检查与相对链接检查；Go 侧库的现状来自检索，成熟度未逐一核实。

## [2026-10-10 09:30] | Task: Linux Wayland W0 只读实验

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> libpipewire 缺失就不加载；开始实现（W0），用户在授权对话框中确认。

### Changes Overview

Scope: Rust 实验原型与设计文档。

- 新增 `experiments/LinuxWaylandProbe`：门户 RemoteDesktop + 窗口 ScreenCast 会话、`ConnectToEIS`、读取设备与区域，不模拟输入。
- 在 GNOME 50 上确认窗口源可用、libei 绝对指针区域与窗口流共享 `mapping_id` 且以窗口为参照系；XWayland 窗口可用 `GetImage` 截取；记录对话框无父窗口不前置、`mutter-x11-frames` 干扰按标题解析两个问题。
- 设计文档写入 W0 结论，更新待确认问题与 GNOME 行；libpipewire 缺失时截图可执行文件不启动、其余能力不受影响。

### Design Intent

先只读验证最大的不确定项（Wayland 指针坐标参照系），再决定是否需要 GNOME Shell 扩展。结果表明 GNOME 上不需要扩展即可按窗口坐标寻址，但目标绑定与遮挡送达仍需在隔离会话中验证。

### Files Modified

- [W0 原型](../../../experiments/LinuxWaylandProbe/README.md)
- [Linux 显示后端架构](../../design-docs/linux-display-backends.md)

### Validation

- 原型在真实 GNOME Wayland 会话中运行三次（一次因未找到对话框超时），第三次完整输出；未发送任何输入。Rust release 构建只依赖 libc/libm/libgcc。

## [2026-10-10 10:30] | Task: Linux 显示后端 W1 结构

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 提交并推送到 PR，然后开始 W1。

### Changes Overview

Scope: `apps/OpenComputerUseLinux`、CI、文档。

- Go：新增 `internal/display`（错误、目标、几何、可用性与 `Capturer`/`Injector` 接口），X11 截图、XTEST 输入与 guard 移入 `internal/display/x11` 的 `Backend`；引擎按会话组合后端，对外 API 与行为不变。测试随代码拆分，guard 与 owner 退出测试归入 x11 包。
- Rust：新增 `wayland-helper/`（`open-computer-use-wayland`），Content-Length 分帧、`hello` 握手与只读探测（Wayland 全局对象、门户版本、合成器提示），EOF 时清理退出，畸形帧结束会话。
- Go 端 `internal/display/wayland` 客户端：启动与握手超时、串行调用、取消、崩溃后不重启、关闭时先结束输入再等待退出。
- `doctor` 输出会话报告；Wayland 会话中启动辅助进程并附上探测结果。CI 新增 Rust 测试与构建，Xvfb 步骤覆盖移动后的测试。

### Design Intent

先把"按会话组合后端"的结构和跨语言进程边界建好并测稳，再在 W2/W3 往里加能力，避免能力实现与结构调整混在一起。辅助进程不自动重启，因为它持有的门户会话与按键状态随进程消失，必须如实报告。

### Files Modified

- [显示层类型](../../../apps/OpenComputerUseLinux/internal/display/display.go)、[X11 后端](../../../apps/OpenComputerUseLinux/internal/display/x11/input.go)、[辅助进程客户端](../../../apps/OpenComputerUseLinux/internal/display/wayland/helper.go)、[引擎组合](../../../apps/OpenComputerUseLinux/internal/desktop/display.go)
- [Rust 辅助进程](../../../apps/OpenComputerUseLinux/wayland-helper/src/main.rs)
- [CI](../../../.github/workflows/sdk-check.yml)、[Linux 显示后端架构](../../design-docs/linux-display-backends.md)、架构文档与执行计划

### Validation

- `go vet`、`go test -race`；隔离 X11 中 X11 全局输入与 guard 测试通过；`cargo test` 6 项通过；真实 GNOME 会话中 `doctor` 启动辅助进程并返回探测、退出后无残留进程；缺少辅助进程与 X11 会话两种情况的报告正确。辅助进程 release 体积 1.9 MB（arm64），只依赖 libc/libgcc。

## [2026-10-10 18:55] | Task: Linux Wayland W3 键盘输入

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 提交并推送 W1，然后开始 W3（GNOME 键盘/文本输入）。

### Changes Overview

Scope: Rust 辅助进程、Go 引擎与 SDK 适配、`runtime-go`、测试脚本与文档。

- 辅助进程新增键盘：门户 RemoteDesktop（或仅限隔离测试的 Mutter API）→ `ConnectToEIS` → libei 键盘；运行时加载 `libxkbcommon` 按合成器键位表查找按键，布局缺失字符发送前拒绝；每次发送等待合成器确认；退出时释放按键并停止模拟。
- 引擎：Wayland 下按键前用 AT-SPI 确认目标窗口 ACTIVE；CLI 对 XWayland 窗口仍用 XTEST、首次使用时弹出对话框；SDK 要求先取得 `remoteDesktop` 权限。按键解析移入共享的 `display` 包。
- `runtime-go` 新增 `PermissionDriver`，转发 `getPermissionStatus` 与 `requestPermissions`；Linux SDK 在 Wayland 会话报告并申请 `remoteDesktop`。
- 新增 `testdata/gnome-session.sh` 无头 GNOME 隔离会话、引擎级 Wayland 键盘测试，并更新 SDK 动作测试与原生协议测试的 Wayland 预期。

### Design Intent

libei 只能送达合成器焦点，因此把目标绑定放在每次按键前的 AT-SPI 检查上，无法确认就拒绝；无法输入的字符整体拒绝而不是部分发送。SDK 不在动作中弹窗，授权走显式的权限接口，与 macOS 一致。

### Incident

最初的无头 GNOME 测试会话与真实桌面共用 `XDG_RUNTIME_DIR` 和配置目录，退出时删除了真实会话的 AT-SPI 总线 socket（新启动的应用无法连接辅助功能），并写入了 `~/.local/share/gnome-shell/` 下的状态文件。已改为每个隔离会话使用私有 XDG 目录并验证真实目录前后不变；真实会话的辅助功能需要注销重新登录或重启 `at-spi-dbus-bus.service` 才能恢复。

### Files Modified

- [辅助进程键盘](../../../apps/OpenComputerUseLinux/wayland-helper/src/keyboard.rs)、[键位表](../../../apps/OpenComputerUseLinux/wayland-helper/src/keymap.rs)
- [引擎 Wayland 路由](../../../apps/OpenComputerUseLinux/internal/desktop/display.go)、[SDK 适配](../../../apps/OpenComputerUseLinux/sdk.go)、[runtime-go 权限](../../../packages/runtime-go/desktop.go)
- [隔离 GNOME 会话](../../../apps/OpenComputerUseLinux/testdata/gnome-session.sh)、[SDK 动作测试](../../../protocol/linux-desktop.test.mjs)
- [Linux 显示后端架构](../../design-docs/linux-display-backends.md)、SDK/协议 README、changeset

### Validation

- 隔离 GNOME：引擎 Wayland 键盘测试 6 次、SDK 动作测试 3 次通过；辅助进程被 SIGKILL 后按键已释放。隔离 X11、原生协议测试、`go test -race`、`cargo test` 通过。
- 真实 Wayland 桌面上的 SDK 测试待辅助功能总线恢复后补跑；CI 未接入无头 GNOME。

## [2026-10-12 10:35] | Task: CI 接入无头 GNOME Wayland 测试

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 先补充 CI。

### Changes Overview

Scope: CI 与测试脚本。

- Linux job 新增步骤：安装 `gnome-shell`，在 `testdata/gnome-session.sh` 中运行 `TestGNOMEWaylandKeyboard` 与 SDK 动作测试。
- 两个隔离会话脚本的清理改为先卸载私有运行时目录中会话服务挂载的 FUSE 文件系统、再删除，且清理不再覆盖命令自身的退出码（此前 `set -e` 下的清理失败会让通过的测试报告失败）。

### Validation

- 本地照跑两个 CI 步骤主体（GNOME 50、Xephyr）均通过；真实运行时目录前后不变，无残留目录与挂载，真实会话辅助功能正常。远端 `ubuntu-24.04`（GNOME 46）尚未运行。

## [2026-10-12 13:55] | Task: Sway/Hyprland 隔离测试会话

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 用户已安装 Sway 与 Hyprland；先写 `wlroots-session.sh`。

### Changes Overview

Scope: 测试脚本与文档。

- 新增 `testdata/wlroots-session.sh`：无头 Sway（软件渲染），或嵌套在无头 labwc 中的 Hyprland；启动 D-Bus 前设置私有 XDG 目录，只加载自带的合成器配置，按会话标记清理全部进程，保留命令退出码。
- 记录 W2 前置探测结果：两者都有虚拟键盘/指针、输入法 v2 与窗口列表；Sway 只有输出级截图，Hyprland 有按窗口导出；Hyprland 无无头模式。

### Incident

写脚本前的临时探测在私有总线启动后才切换 XDG 目录，私有总线激活的服务看到了真实运行时与配置目录；失败的无头 Hyprland 尝试留下了未退出的进程（已结束）和 `/var/crash/_usr_bin_Hyprland.1000.crash`（交由用户删除）。真实会话的辅助功能 socket 未受损。

### Validation

- 两种合成器各运行多次：探测正常、退出码透传；真实运行时目录、辅助功能 socket、dconf 与 GNOME Shell 状态文件、systemd 用户环境、`/var/crash` 前后不变，无残留进程与临时目录。

## [2026-10-12 15:40] | Task: W2 wlroots 虚拟键盘

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 提交并推送到 PR，然后开始 W2。

### Changes Overview

Scope: Rust 辅助进程、Go 引擎与 SDK、测试脚本、CI 与文档。

- 辅助进程新增 `wlr.rs`：虚拟键盘（自生成键位表、固定的基础键码、修饰键按下后上报修饰状态、创建与输入前清零修饰状态、释放按键）与 Sway/Hyprland IPC 焦点窗口查询。
- 引擎：合成器提供虚拟键盘时走该路由，不需要授权；每次按键前要求 IPC 的焦点窗口为目标 PID 与标题，无 IPC 时回退 AT-SPI ACTIVE。SDK 在该路由下不报告 `remoteDesktop` 权限。
- `wlroots-session.sh` 保持一个座位键盘并缩短私有根目录；新增 `TestWlrootsVirtualKeyboard`；GNOME 与 X11 测试改为在无选区文本上验证 Ctrl+A；SDK 动作测试适配无授权路由；CI 新增无头 Sway 步骤。
- 修正测试路由：隔离测试会话使用 Mutter 授权时，键盘可用性不再要求门户（远端 GNOME 46 运行器上因此失败）。

### Design Intent

wlroots 合成器开放虚拟键盘和带 PID 的 IPC，能把输入绑定到具体窗口，比 GNOME 门户更强也不需要对话框。键位表固定基础键码，是因为实验证明客户端会用旧键位表解析快捷键。

### Validation

- 隔离 X11、GNOME、Sway、Hyprland 中引擎与 SDK 测试全部通过；Sway 与 Hyprland 各 3 次重复通过；真实桌面的 SDK 与原生协议测试通过；真实会话前后不变、无残留。
- 修饰键实测：最初的 `modifiers` 请求与键位表 `modifier_map` 都未生效（GTK 记录到状态 0），改为先按修饰键再上报后生效；跨设备的键码复用会让 Ctrl+A 失效，固定基础键码后通过。

## [2026-10-12 18:10] | Task: 修复无头 GNOME CI

### User Query

> 等 CI 结果，失败了就修；之后改用 ubuntu-26.04 运行 GNOME 键盘测试。

### Changes Overview

- 辅助进程在 `ei_seat.done` 之后才绑定键盘能力，并在等待超时时报告缺少的环节；GNOME 46 上授权因此成功。
- 隔离 GNOME 会话以 unsafe mode 启动并通过 `Shell.Eval` 让新窗口获得焦点，预热键盘在授权后轻按 Shift；测试在窗口未激活时输出外壳与 AT-SPI 的焦点信息。
- 诊断显示 GNOME 46（`ubuntu-24.04`）的外壳已聚焦夹具窗口，但 GTK 始终不报告 ACTIVE，按键按设计被拒绝；GNOME 键盘测试移到独立的 `ubuntu-26.04` 任务（GNOME 50，与本地一致）。

### Validation

- GNOME 50 本地照跑新任务的步骤通过；远端 `ubuntu-26.04` 任务待首次运行确认镜像可用。
- 后续：`ubuntu-26.04` 上同样失败；诊断显示座位已有键盘、外壳焦点是夹具窗口。GTK 3 只在收到 `wl_keyboard.enter` 后变为 ACTIVE，而外壳启动时处于 overview，键盘焦点留在其搜索框。焦点处理改为先退出 overview 再激活窗口后，CI 全部通过。

## [2026-10-12 19:00] | Task: W2 wlroots 截图与虚拟指针

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> CI 过了之后继续 W2 剩下的部分。

### Changes Overview

Scope: Rust 辅助进程、Go 引擎与 SDK、测试脚本、CI 与文档。

- 辅助进程：`window.find`/`window.capture`（Sway IPC + screencopy 区域截图，有浮动窗口重叠时拒绝；Hyprland IPC + toplevel export，协议文件取自 hyprland-protocols v0.7.1，BSD-3）；新增 `pointer.rs`：`zwlr_virtual_pointer` 绝对移动（`xdg_output` 布局外包框）、按键、点击与释放，设备常驻。
- Go：`internal/display/wayland` 新增窗口查找、截图（共享内存帧转 PNG 并缩放到逻辑尺寸）与指针调用；引擎在 Sway/Hyprland 上为原生 Wayland 窗口提供截图、`WindowRect`、点击、拖拽，点击前移动指针并要求 IPC 焦点为目标；元素中心用窗口坐标系 extents 加窗口原点。SDK 的截图与指针可用性来自引擎。
- 测试：新增 `TestWlrootsPointerAndCapture`（截图尺寸与内容、元素单击与双击、拖拽、窗口外拒绝、浮动窗口遮挡时拒绝点击与截图）、截图格式与缩放单元测试；SDK 协议测试按指针可用性断言；CI Sway 步骤改名为输入与截图。

### Design Intent

指针绑定目标的依据是合成器 IPC：移动后焦点跟随鼠标落到目标窗口，才按下按键。这样不需要合成器暴露「指针下方是哪个窗口」，代价是关闭焦点跟随鼠标的配置会被拒绝。截图按逻辑尺寸输出，使截图坐标与指针坐标在缩放输出上一致。

### Validation

- 隔离 Sway 中引擎测试连续 5 次通过，SDK 协议测试通过；隔离 X11 中引擎与 SDK 测试通过（无回归）。
- 实测问题：无头会话没有物理指针，每次手势新建虚拟指针会让座位指针能力反复出现，GTK 异步绑定期间的点击偶发丢失；改为设备常驻并在会话中预热后稳定。
- Sway 窗口几何修正：IPC 的 `rect` 包含边框，客户端内容需加 `window_rect` 偏移（实测 1024x743 → 1020x741）；元素坐标前新增 AT-SPI 与合成器窗口尺寸一致性检查。
- Hyprland：嵌套到真实桌面（`OCU_HYPRLAND_PARENT=wayland-0`）后有显示器，引擎测试连续 3 次通过、SDK 测试通过，截图人工检查正确；遮挡测试覆盖两种合成器。真实会话、`/var/crash` 前后不变，无残留。

## [2026-10-13 12:50] | Task: 打包 Wayland 辅助进程

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 开始做打包。

### Changes Overview

Scope: 构建脚本、npm 打包与验证、CI、第三方声明与文档。

- `build-open-computer-use-linux.sh` 在 Go 运行时旁构建 `open-computer-use-wayland`。`--portable-helper`（SDK 平台包与 CLI 包使用）要求 cargo-zigbuild 按 glibc 2.28 链接，并检查产物要求的最高 glibc 版本；默认模式在缺少 zig 时退回本机 cargo（需要本机 glibc），跨架构则跳过并警告。
- SDK 平台包、CLI 包与 Codex 插件安装都随运行时携带辅助进程。`verify-sdk-package.mjs` 让安装后的辅助进程完成握手；SDK 分发工作流在 Linux 上安装 Rust、zig 与 cargo-zigbuild，并在 Debian 10 容器中启动辅助进程。
- 新增 `scripts/wayland-helper-notices.mjs`，把辅助进程链接的 96 个 crate 及其许可证全文（相同文本只收一次）与 Hyprland 协议的 BSD-3 声明写入 `THIRD_PARTY_NOTICES.md`；`sdk-check` 与 `scripts/ci.sh` 检查其是否过期，`scripts/ci.sh` 也加入 gofmt、go vet、rustfmt 与 clippy。

### Design Intent

GNOME 键盘路由在运行时 `dlopen` libxkbcommon，静态 musl 程序不能这样做，所以保留 glibc 动态链接，只把版本要求降到 2.28。zig 还能在 macOS 或单台 Linux 上交叉构建两个架构，符合 CLI 包一次构建全部平台的方式。

### Validation

- 本机（Ubuntu 26.04，arm64）用 zig 构建两个架构，要求均为 glibc 2.28；arm64 产物在隔离 Sway 中通过引擎测试。
- `npm run sdk:runtime:build` 与 `npm run sdk:package:verify` 在 linux-arm64 通过，平台包含 `runtime/open-computer-use-wayland`，安装后握手成功。
- 构建脚本的三种退路：缺少 zig 时本机架构退回 cargo（要求 glibc 2.39 并警告）、跨架构跳过、`--portable-helper` 失败。
- 未在本机验证：Debian 10 容器启动（本机无容器运行时，由 CI 验证）、x64 机器上的实际运行、macOS 上的 CLI 包构建。

## [2026-10-13 14:40] | Task: GNOME 截图与输入方案（Shell 扩展）

### Execution Context

- Agent ID: `/root`
- Base Model: Claude Opus 5.5
- Runtime: Claude Code (desktop app)

### User Query

> 先做A，不用隔离；输入用扩展里的虚拟设备，先做验证；写进设计文档。

### Changes Overview

Scope: 设计文档与执行计划，无代码改动。

- 比较 moeru-ai/auv（门户显示器流 + 按 gnome-shell「Main stage」树裁剪）与 trycua/cua（WinRects Shell 扩展）。在真实桌面上只读实验 Main stage 树：节点顺序即层叠顺序，ACTIVE 窗口总在最后，但节点没有 PID 与标题；`Shell.Introspect.GetWindows` 对第三方返回 `AccessDenied`。
- 决定 GNOME 改用自有 Shell 扩展（W4），输入使用扩展内的 Clutter 虚拟设备，Shell 内授权；不实现无扩展的显示器裁剪退路。
- 在隔离的无头 GNOME 50 中用 `Shell.Eval` 验证：窗口列表、被完全遮挡的窗口截图、落点窗口、同一调用内确认落点后的虚拟指针点击、虚拟键盘、`Main.inputMethod.commit` 提交任意 Unicode。

### Design Intent

扩展能在 gnome-shell 内确认窗口身份与落点，并把确认与送达放在同一次调用中，这是门户方案给不出的目标绑定。GNOME 有意收紧了截图与窗口列表接口，所以扩展只接受本产品辅助进程，并在首次使用时由用户在 Shell 内授权。

### Validation

- 真实桌面实验只读取无障碍树，打开过两个测试窗口并让其申请焦点，未注入输入。
- 隔离会话：PID 与夹具一致；遮挡时截到的是目标窗口本身；遮挡时点击被拒绝、置顶后计数 0 → 1；按键送达焦点控件；布局外键符被 Mutter 丢弃，经输入法提交的 `中文 😀 é` 原样进入输入框。会话结束后无残留。

## [2026-10-13 15:00] | Task: PR 收尾

### User Query

> 好，做收尾，然后提交并推送到 PR。

### Changes Overview

- `doctor` 与 `help` 的说明按现状改写：全局输入先确认目标；X11/XWayland 走 XTEST；Sway/Hyprland 走辅助进程的截图与虚拟键盘、指针；其他 Wayland 桌面只有门户键盘。
- 执行计划把 Python 删除一项标为完成，依据是 SDK 分发工作流在两个 Linux 架构上的干净安装与启动；macOS 上构建 CLI 包仍未覆盖。
- 更新 PR 标题与描述（打包、GNOME 扩展方案、未覆盖项）。
