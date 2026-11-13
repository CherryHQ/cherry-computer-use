## [2026-11-09 05:42] | Task: 评估上游分歧并移植必要修复

### 🤖 Execution Context
* **Agent ID**: `/root`
* **Base Model**: `gpt-6-astra`
* **Runtime**: T3 Code / Codex harness

### 📥 User Query
> 解决和 upstream 的冲突，看看上游改了什么，有必要吗。

### 🛠 Changes Overview
**Scope:** macOS 原生引擎与 CLI/MCP、Windows runtime、Linux CLI 参数、回归验证及同步文档。

- 确认 Cherry 当前主线与远端一致；原项目上游包含 75 个独有提交，试合并出现 30 个文件冲突。
- 选择性移植 macOS 点击候选范围、CLI/MCP 实时焦点与选区输入、异常坐标保护、请求 autorelease pool、相对调用文件路径，以及 macOS/Windows 写值读回和 Windows CLI 目标窗口截图；Windows/Linux CLI 的 `set_value` 保留字面空白。
- 保留 Cherry 的 SDK 协议、应用会话、CDP、Go Linux 引擎、品牌和发布体系；不将尚未接入的新功能标记为已合并。
- 为上游新增能力、重复修复和后续需要适配的修复建立[评估记录](../../references/upstream-reconciliation-20261109.md)。
- 补充 patch Changeset、架构与用户发布说明；Windows 函数测试接入 SDK CI。

### 🧠 Design Intent (Why)

上游已有可直接复现的误点击、旧焦点输入、写值假成功和异常几何崩溃修复，值得吸收。Locked Use、虚拟显示器、JS REPL 等是独立产品能力；上游 Python Linux 后端、发布配置也与 Cherry 明显不同，直接整文件覆盖会损失现有实现。

本轮是选择性移植，没有完成完整 upstream merge。focused sheet、可见节点预算、批量 AX 读取、光标跟随及 Windows DPI/子窗口点击保留为后续适配项。

### 验证

- 基线 `swift test`：206 项，1 项按开关跳过，0 失败。
- 修改后 `swift test`：222 项，3 项真实交互测试默认跳过，0 失败。
- 显式运行 `UpstreamNativeInputTests`：2 项独立 AppKit 真实窗口回归通过。覆盖空白/窗口点击不误触其他控件、目标按钮单击、选区与 Unicode 插入、实时焦点切换、焦点不可读、写入失败不重发、延迟/忽略/裁剪写值、清空输入。
- `scripts/run-tool-smoke-tests.sh`：现有 9-tool fixture smoke 与 cursor idle smoke 通过。
- SDK build、类型检查和 47 项客户端/发行包测试通过。
- Windows runtime `go test ./...`、共享 Go runtime `go test -race ./...` 通过；Windows arm64/x64 交叉构建通过。
- Linux 模块全套 Go 测试在规范化临时目录后通过：macOS 默认临时路径含符号链接，先前运行有两项 desktopentry 路径比较失败；设置规范化的 `TMPDIR` 后全部通过。未执行 Linux 真实桌面测试。
- PowerShell 7.6.9 在 macOS 上执行生产函数测试：字面/空字符串、忽略与裁剪写入、只读与单次写入，以及无效/移动/缩放/隐藏/最小化窗口几何通过。此项使用模拟 UIA/Win32 对象。
- 7 项发布脚本测试、版本一致性、文档骨架、仓库卫生、Action SHA 固定与差异检查通过。
- `actionlint` 与未修改的基线对比没有新增诊断；仍有本机工具不识别 `ubuntu-26.04` 和既有 shellcheck 提示，不记为完整 lint 通过。

### 限制

- 当前机器辅助功能已授权，录屏未授权；AppKit 回归显式关闭截图，没有实际验收 SCK 截图。
- Windows 原生桌面截图与跨 DPI 输入尚未验收；模拟 PowerShell 测试和交叉构建不能代替这些验证。
- 远端 CI 未运行，不声称已完成远端同步或合并。

### 📁 Files Modified
- `packages/OpenComputerUseKit/Sources/OpenComputerUseKit/`：输入、几何、写值读回、CLI 路径和请求资源释放。
- `apps/OpenComputerUse/Sources/OpenComputerUse/MacOSAppAgentProxy.swift`
- `apps/OpenComputerUseLinux/cli_engine.go`：保留 `set_value` 字面参数。
- `apps/OpenComputerUseWindows/`：写值验证、字面参数、目标窗口截图与 Go 回归测试。
- `packages/OpenComputerUseKit/Tests/OpenComputerUseKitTests/`、`scripts/tests/`：单元与独立 AppKit fixture 回归。
- `.github/workflows/sdk-check.yml`、`.changeset/quiet-upstream-input-fixes.md`
- `docs/ARCHITECTURE.md`、`docs/QUALITY_SCORE.md`、`docs/releases/feature-release-notes.md` 与同步评估记录。

### 2026-11-13：新增能力复评

用户认为 Locked Use、虚拟显示器、后台操作、query/select_text、按需观察、截图配置和效率改进都重要，要求进一步核对。

- 刷新 Cherry 主线至 `2d9bd95`、upstream 至 `bef6c05`；保留本地 `cb192a1`，未合并或改动运行时代码。
- 逐项核对当前协议与原生调用路径：SDK 已有不截图、`perform()` 和结构化权限查询；缺少 query/select_text、纯截图模式、非 PNG 格式及后台窗口/锁屏能力。
- 核对上游真实测试记录和当前 PR/issue：虚拟屏显示排列修复 #114 尚未合并；Karabiner #142 仍开放；新暗屏修复 #187 的签名完整锁屏流程尚未实测。
- 新增[能力接入复评](../../references/upstream-feature-adoption-20261113.md)，修正“第一批只修 bug”可能被理解成长期排除能力的表述，给出 SDK 适配、生命周期归属与分批验收方案。
- 本轮为源码评估，没有安装系统组件、触发锁屏或创建显示器；`make check-docs`、修改文档的相对链接检查和 `git diff --check` 通过。未重跑运行时测试，也不把上游验证记录当作 Cherry 的实机验收。


### 2026-11-13：用户确认后的能力接入

用户确认“好的，开始接入”。先整合 Cherry `2d9bd95`，保留此前修复与应用图标 API，再按功能移植 upstream `bef6c05`；虚拟屏布局与自动移窗恢复另固定取自 PR #114 的 `3dedad7`。不是完整 upstream merge，JS REPL、DeepSeek Harness、上游 Python/Linux 与发布流程未引入。

- macOS CLI/MCP 增加 query/select_text，共 11 个工具；SDK 查询结果进入应用会话快照，文本选择进入 act/perform，继续严格校验 appSessionId/snapshotId/elementId。
- 三端按需观察真正跳过未请求的 AX/截图通道；macOS 支持 PNG/JPEG/无损 WebP、尺寸和 JPEG 质量，SDK 返回实际 MIME，CLI 使用 Cherry 独立配置。新增 doctor JSON 诊断。
- 接入 AX 批量读取、可见控件预算、密码值保护、窗口光标跟随、后台窗口及 sky_key。Windows 保留 Cherry HWND/UWP/CDP 路由，新增 PID/句柄/完整元素身份校验和动作已完成但观察失败的区分；Linux 将预算算法适配到现有 Go AT-SPI 引擎。
- macOS SDK 显式启用任务虚拟屏/Locked Use；停止应用只释放所属资源，任务关闭恢复窗口并确认保护释放。移窗前记住原位置，停放和恢复均要求确认；未确认清理返回 CLEANUP_FAILED。
- Broker、Guardian、installer、认证插件改用 Cherry 身份/路径，正式 Developer ID 包捆绑四组件并校验签名和架构；构建不安装服务。libwebp 精确依赖与许可证随制品分发。
- 真实 AppKit 回归发现无 AXIdentifier 的 legacy 文本控件返回通用 failure，修复这一可选属性的兼容性；已知标识消失及角色/名称变化仍拒绝操作。原有坐标命中测试使用合成 1:1 图像帧，只证明输入，不冒充 SCK 截图验证。
- 协议 schema、生成器、TS 客户端、Swift/Go 分发、既有 CI、minor Changeset、架构/SDK/安全/发布说明同步更新。

验证结果：

- 显式开启独立 AppKit 回归后的完整 Swift 测试：536 项，9 项特殊实机测试跳过，0 失败。三个 AppKit 回归包含真实 SDK query → selectText → typeText 的 Unicode 选区替换、实时焦点/写值读回、空白/窗口点击不误触。
- 11-tool 注册、原有工具和 select_text headless MCP smoke，以及 cursor idle smoke 通过。
- SDK 构建/类型检查与 52 项测试、7 项原生私有会话/断连清理协议测试通过。
- 共享 Go runtime 与 Linux 模块 race 测试、Windows Go 测试通过；Windows x64/arm64 编译通过。
- PowerShell 生产函数测试覆盖观察通道、Chromium/密码选区保护、元素身份、窗口/PID 变化、键盘投递、动作与后续观察失败、写值和截图几何；在 macOS 的 pwsh 中运行模拟依赖，非 Windows GUI 证据。
- Cherry ad-hoc app 构建通过；认证插件编译及离线 ABI 拒绝/取消/重放测试通过，没有安装组件或求值系统授权规则。
- 发布脚本、版本一致性、Changeset 状态、修改文档相对链接、文档/仓库卫生、固定 Action SHA 与 diff 检查通过。actionlint 与基线同为 10 条既有诊断，无新增，不记为完整 lint 通过。正式签名、公证和远端 CI 未执行。

剩余边界：本机没有 Developer ID 证书和录屏授权，未安装系统组件、触发锁屏或创建虚拟屏。完整锁屏/暗屏/物理接管、安装升级卸载、多物理显示器/跨 Space、实际 SCK 图片出口及 Windows/Linux GUI 留作独立验收，已登记到技术债。源码接入与这些实机结果分别陈述。

实现与调用说明见[新增桌面能力](../../design-docs/upstream-desktop-capabilities.md)，完整过程见[执行计划](../../exec-plans/completed/20261113-upstream-feature-adoption.md)。

### 2026-11-13：后台任务恢复与 Locked Use 加固

用户确认由 Cherry 自己修复后台操作、虚拟屏和 Locked Use 的剩余问题，并要求继续推进。

- 明确窗口的观察/查询只读取该窗口内的焦点和选区，防止暴露同应用另一文档的选区；显式查找包含失去焦点的 `AXSheets` 和原生保存弹窗。
- 新增私有恢复进程，在创建虚拟屏或移窗前持有原始位置与 AX 引用，并自己设置/记录原遮挡通知状态。继承管道 EOF 覆盖任务进程 SIGKILL；恢复逐窗口执行，身份暂时不可读保留记录，已退出或归属变化的目标不重新猜测。
- 实机回归发现释放 `CGVirtualDisplay` 对象不足以移除重新排列过的虚拟屏，改由恢复进程的独立显示器子进程完成创建和排列。先恢复窗口，再确认显示器进程退出与在线枚举中移除。在线位查询会受 CoreGraphics 进程缓存影响，不能独自当作退出证据。
- 虚拟屏使用独立标识，保留原显示器位置，放在最外侧右下角接触的位置。Quartz 会自动消除显示器之间的空隙，因此没有保留最初尝试的间隔方案，也不承诺鼠标绝对无法进入。
- SDK 在申请任务保护前登记取消；失败/取消的观察回滚本次新增的停放和遮挡状态，位置清理失败仍尝试其他资源。停止与关闭保留失败依据，并等待相应任务清理预算。
- HID 启动期间出现输入时拒绝 readiness，Input Monitoring 撤销后永久失效直至重建；新增不锁屏、不解锁的聚合探针。避免在每个高频 HID 回调查询 TCC 权限。暗屏唤醒和 guard 释放等待可被取消，未授予租约时释放显示保活。
- 修复 Locked Use 签名脚本在 macOS 系统 Bash 下展开空数组时退出的问题；补 patch Changeset、架构/安全/稳定性/发布说明及实机验收边界。

本轮已经获得可用 Developer ID 身份，与前一阶段“无证书”的环境不同。release arm64 CLI 与四组件完成签名构建，离线认证插件 ABI 与真实签名内核验证通过；未执行公证、系统组件安装或真实锁屏。

验证结果：

- 完整 Swift 回归 551 项，8 项按显式开关/权限条件跳过，0 失败；开启独立 AppKit、虚拟屏 fixture 和原生电源断言检查。
- 11-tool 注册、fixture 动作和文本选择 smoke、cursor idle smoke 通过。
- 八项 AppKit 实机回归包括真实保存弹窗、跨文档焦点隔离、查询/选区/输入、普通窗口强杀恢复，以及真实 SDK 虚拟屏停放/停止恢复和虚拟屏上窗口强杀恢复。检查窗口原位置、既有屏幕原点、独立显示器移除；AX 与 WindowServer 的 backing-scale 舍入允许 1 点误差。
- SDK 类型检查、构建和 52 项测试通过；签名 release app 的 7 项原生协议测试通过，包括重复会话关闭、EOF、异常 framing、私有 agent 隔离和代理 SIGKILL 清理。
- 签名四组件的架构/同团队/identifier/严格签名检查通过；认证插件离线拒绝/取消/重放、内核正确身份及错误团队/identifier/ad-hoc/debug entitlement 拒绝、Guardian peer/角色检查通过。
- 签名 Guardian HID 探针观察到 11 个可监控设备、6 个被独占设备，健康状态通过；探针不锁屏、不解锁，不证明 Karabiner/Secure Input 下每个物理设备都能触发接管。
- 发布脚本 7 项、版本一致性、Changeset 状态、文档/仓库卫生、Action SHA、修改文档相对链接和 diff 检查通过。

仍需独立验收：签名组件安装/升级/卸载、完整锁屏/暗屏与 Karabiner 物理接管、多物理显示器/跨 Space/热插拔；本机录屏仍未授权，Chrome 截图回归明确跳过，未把 AX fixture 当作 SCK 图片证据。没有推送、远端 CI、公开发布或公证。后续边界继续保留在技术债记录中。

执行与验证细节见[恢复计划](../../exec-plans/completed/20261113-desktop-resource-recovery.md)。

### 2026-11-13：跨 Space 验收准备

用户要求继续验收跨 Space 场景，并选择开启权限后进行完整验收。

- 将真实 Chrome 跨 Space 回归拆分为控件树/输入和截图两项，录屏未授权不会阻塞前一项；通过 AX 窗口标题和 Window ID 绑定目标，不依赖受录屏权限约束的 CG 标题。
- 在切换桌面前记录原 Space 和前台应用，所有退出路径恢复它们并释放遮挡维护。只选同显示器的普通 Desktop，明确断言目标窗口离屏、操作期间 Space/前台应用/鼠标不变。
- 补充截图解码与测试页面颜色验证、SDK query/selectText/typeText 和停止清理检查。
- 编译与文档/diff 检查通过。显式运行后两项均按前置条件跳过：测试身份没有录屏权限，当前显示器只有一个 Desktop；未报告为实机验收通过。设置列表中的 Cherry 授权开关与当前签名程序的运行时状态不一致，仍需复核实际授权身份。

验收继续保留为[进行中的计划](../../exec-plans/active/20261113-cross-space-acceptance.md)，等待真实 Desktop 和权限条件满足。

后续授权与实测：

- 重新绑定当前 Developer ID 制品的辅助功能/录屏授权并重启，使用新的 LaunchServices app agent 和 SDK 私有 agent 确认两项权限均正常；不将禁用 proxy 后继承测试宿主权限的结果算作签名应用授权。
- 新增公开 SDK 的 macOS 实机验收，直接运行签名制品和独立 AppKit fixture。PNG/JPEG/WebP 实际返回 640×463 图像，独立解码、尺寸限制和非空像素检查通过；按需采集的缺省通道状态符合协议。query/selectText/skyKey 后 fixture 读回 `hello 跨Space🙂`；停止/关闭完成且目标进程保留。
- Chrome 完全遮挡下的后台键盘用例通过，前台应用/窗口焦点计数和鼠标位置均保持不变。第一次运行伴随鼠标移动和 key-window 变化而失败，保持桌面静止后的复测通过；没有删掉或放宽断言。
- 跨 Space 用例按实际 SDK 契约从 `snapshot.tree.elements` 读取查询结果，并在点击按钮后显式点击输入框，避免把按钮获得焦点误判为键盘无法输入。
- 两块物理屏幕各一个普通 Desktop，仍未满足跨 Space 前置条件；两项测试继续明确跳过。真实 SDK 截图通过不等于跨 Space、SCK 专属回退、完整锁屏或多屏虚拟显示器验收通过。

继续验证自动准备桌面：

- 直接使用 Dock 辅助功能树和调度中心通知，实测创建、切换、删除临时普通 Desktop；测试准备器只删除本次创建且已无窗口的桌面，不修改 SIP、不注入 Dock，也不进入普通 SDK 任务启动路径。
- 跨 Space 的 Chrome 控件树、点击、键盘和 SDK 查询读回成功；发现选区写入的异步读回问题。Chromium 在写入返回后约 100 毫秒才发布新选区，改为一次写入后最多等待一秒，期间持续检查原文字及任务有效性。12 项选区测试通过。
- 新一轮两项跨 Space 测试有三处失败：一项未读到调度中心桌面列表，另一项完成 SDK query/selectText/typeText 后观察到前台应用和鼠标位置变化。用户反馈交互受影响后停止桌面切换测试；已确认临时桌面清理、原桌面恢复且无验收进程残留，不将此轮报告为完整验收通过。
- 这轮修复前的完整 Swift 基线为 552 项、18 项按显式开关跳过、0 失败，签名原生协议 7 项通过。修复后再次完成无桌面操作的编译与回归：11 项选区测试通过，两项跨 Space 测试因未开启显式开关而跳过；调度中心准备器的最新等待逻辑仅完成编译检查，未恢复交互式复测。
- 当前代码已重新构建 macOS arm64 release CLI/app 和四个 Locked Use 组件，Developer ID 签名、架构/身份验证、离线认证插件 ABI 和内核签名验证通过；没有安装系统组件或修改授权规则。新增选区等待逻辑后的完整 Swift 回归、原生协议复测和跨 Space 截图验收仍未完成。

### 2026-11-13：Windows/Linux 同步复核与 Windows 输入补齐

用户追问 Windows/Linux 上游改动是否已吸收。复核现有提交后确认两端已接入按需采集、可见控件预算与密码保护；Windows 另有目标截图、写值读回、定向按键、元素身份、隐藏 PowerShell 与动作/观察结果区分；Linux 保留 Cherry Go/AT-SPI/X11/Wayland/CDP。

- 找到尚未完整适配的上游 `5136415` 和 `4dbaa7a` 输入修复：Windows 滚轮错误转换成客户区坐标、左右方向相反；经典点击未命中子 HWND，且未建立统一的物理 DPI 上下文。
- 在 CLI/SDK 共用桥接初始化时建立并验证 per-monitor 物理坐标，失败停止；滚轮保留有符号屏幕坐标并验证投递结果。
- 经典点击在目标窗口内部逐层命中同进程、可用子控件，复核根窗口及子控件几何，投递失败保留按钮释放；原 UWP/Chromium/CDP 路由保留。现有宿主应用指针路径改为整个虚拟桌面的坐标归一化，支持左侧/上方显示器。
- 新增直接执行生产 PowerShell 函数和 C# 算法的 Win32 模型回归，覆盖嵌套/隐藏/禁用/跨进程子控件、移动后拒绝、失败释放、滚轮符号和方向、DPI 拒绝及多屏坐标；接入既有 Windows CI 测试入口。
- Windows Go 回归、Linux/共享 Go race 测试、Windows amd64/arm64 编译及全部八组 PowerShell 函数回归通过。后者使用 macOS 上的 pwsh 和模拟 Win32，不能作为 Windows 混合 DPI/真实控件实机验收。

实现与边界见[Windows 输入补齐计划](../../exec-plans/completed/20261113-windows-upstream-input-gaps.md)。

### 2026-11-14：修复 Windows SDK CI 启动失败

用户要求修复 PR #37 的 CI。核对 head `0e7581f` 的失败日志，Windows 原生冷启动在执行 `sdk.ps1` 前即因 `Observe-SDK` 的缺失括号解析失败；此前函数回归只解析了 `runtime.ps1`，未覆盖 SDK 适配脚本。Linux 各任务已通过，两个 macOS 任务为取消状态且没有步骤或日志，未将它们归为代码失败。

- 修复省略控件树响应分支的括号，并拆成多行以清楚区分哈希表与条件分支。
- 新增三个内嵌 bridge 脚本的完整解析检查，以及生产 `Observe-SDK` 默认、仅树和仅截图模式回归；验证采集调用、返回状态、窗口元数据、元素引用及截断标记。
- CI 在 Windows 原生启动前使用 Windows PowerShell 执行新回归，原 pwsh 引擎测试入口也包含它；没有移除或放宽既有桌面测试。
- 新回归对旧提交复现与 CI 相同的 parser error；修复后全部九组 PowerShell 回归、Windows Go 测试与 amd64/arm64 编译通过。PowerShell 本地结果使用 macOS pwsh 和受控依赖，Windows PowerShell 与真实桌面结果由新 CI 验证。
- 文档/仓库卫生和 diff 检查通过，workflow lint 与修改前相同，保留十条既有诊断、无新增诊断。

### 2026-11-14：隔离 Windows CI 测试替身

后续 head `09acdcf` 的 Windows CI 已通过 Windows PowerShell SDK 观察、真实原生冷启动及 Go 回归，随后在引擎函数测试中失败：同一 pwsh 进程先运行截图再运行选区测试，两者以 `Add-Type` 定义不同的 `OCUWin32`，导致类型重名。此前本地逐脚本启动独立进程，没有复现 CI 的共用进程方式。

- 新增统一 `windows-runtime.test.ps1` 入口，为每个顶层 suite 启动当前 PowerShell 引擎的独立进程；子进程失败立即使入口失败，保留所有原有断言。
- CI 与本地改用同一入口，避免执行方式漂移；不修改生产输入或观察逻辑。
- 本地以原 CI 共用进程命令复现相同的类型冲突；新入口全部九组回归通过。另用受控子脚本验证非零退出码会传播且后续 suite 不再执行。
- 文档/仓库卫生和 diff 检查通过；workflow lint 无新增诊断，仍保留十条原有诊断。真实 Windows 后续结果由新 CI 验证。
