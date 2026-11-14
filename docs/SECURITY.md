# 安全默认约束

## 当前实现边界

- 对 MCP host 暴露的接口仍是本地 `stdio`；macOS CLI 与 `.app` app agent 之间会使用用户临时目录下的 Unix domain socket，socket 创建后会收紧为当前用户读写，且不对外监听 TCP/HTTP 端口。未设置 `OPEN_COMPUTER_USE_AGENT_SOCKET_NAMESPACE` 时继续使用历史 Socket；设置后仅以 namespace 摘要派生私有文件名，不把宿主目录或原始 namespace 写入 Socket 路径。
- CLI/MCP 动作必须显式带 `app`；SDK 动作绑定 `appSessionId` 与 `snapshotId`，元素还需匹配该快照的 `elementId`。查询不能跨会话复用引用。
- macOS 真实 app 路径依赖 `Cherry Computer Use.app` 已获得 `Accessibility` 与 `Screen Recording` 权限；终端里的 CLI / Node launcher 会把 `mcp`、`doctor`、`call`、`snapshot` 和 `list-apps` 转发给由 LaunchServices 启动的本地 app agent，避免把权限要求落到 iTerm / Terminal 身上。
- 实验性 Linux runtime 依赖已登录桌面用户的 AT-SPI2 / D-Bus session；coordinate mouse、drag、keyboard synthesis 只是 best-effort fallback，不应被视为跨 Wayland compositor 的通用后台输入授权。

## 数据处理

- 普通 app 的 screenshot 默认只在内存中编码成 PNG；macOS 可明确选择 JPEG 或无损 WebP，并通过 MCP `image` content block 直接回传；默认不长期持久化。
- Linux runtime 的 screenshot 是 best-effort；如果 GNOME Wayland 返回黑图，bridge 会省略 image block，避免把无效截图误当成真实画面。
- fixture app 的合成状态只写到本地临时 JSON 文件，目的是支撑 deterministic smoke test；当前写入走原子替换，减少测试期间的读写竞争。
- 当前仓库不引入第三方服务，也不上传截图、AX tree 或输入内容。

## 授权与最小权限

- 当前只保留一层密码管理器 bundle denylist / bundle-id gate：
  - 会阻止对 1Password、Bitwarden、Dashlane、LastPass、NordPass 和 Proton Pass 做直接 `get_app_state` / action 调用。
  - 终端类 app、Chrome / Atlas 和系统组件不再属于内置阻止目标。
  - 对 bundle identifier 直传时返回 safety denial；对 app name 查询时默认不把这些密码管理器暴露成可解析目标。
- 但当前仍然没有官方闭源实现里的 session approval / 动态 app policy。
- 这意味着开源版当前的安全边界主要由：
  - 明确的 tool 调用参数
  - 内置密码管理器 denylist
  - `Cherry Computer Use.app` 的系统权限
  - 本地使用场景
  共同提供。
- `click_method=global` 是显式的系统级指针路径，可能移动真实鼠标、改变前台焦点或命中坐标处的其他窗口。调用参数本身不视为足够授权；macOS 和支持该模式的 Linux runtime 还要求进程环境中设置 `OPEN_COMPUTER_USE_ALLOW_GLOBAL_POINTER_FALLBACKS=1`。未设置时必须在任何可见 cursor 移动或真实输入事件之前拒绝请求。
- `click_method=app_post`、`sky_click` 与 `accessibility` 不允许静默切换到 `global`。这保证调用方选择的非侵入边界在失败时仍然成立。
- `click_method=sky_click` 是显式 macOS 私有 SPI 能力，不进入 `auto`。它不移动系统指针、不改变 WindowServer frontmost app，也不 raise 或切换目标窗口；内部只让目标应用短暂进入 synthetic-active 状态，绝不向真实前台应用发送 defocus record，renderer settle 后也只撤销目标的合成状态。点击后的 action-result snapshot 禁止 activate / `AXRaise` 恢复。它仍会向指定 PID/window 注入真实输入语义，因此必须使用当前 snapshot 已验证归属、同 PID 的窗口，并在窗口身份不匹配、target-focus record 失败或私有符号缺失时 fail closed。仅支持左键单击/双击；遮挡和跨 Space 窗口另由可见性维护支持。
- SkyLight ABI、raw event field 和 Chromium 接收行为都不受 Apple 公共兼容性承诺保护。系统升级后的失败不得触发静默 global fallback；应先重新验证符号和受控目标，再决定是否更新实现。
- 下一阶段应优先补：
  - session 级审批
  - 更清楚的敏感 app / 系统设置防护策略

## 可选任务保护

- macOS `taskOptions.lockedUse` 默认关闭；启用还要求用户独立安装和验证签名组件。普通观察、诊断与 SDK 初始化不会安装系统服务。签名标识、认证插件、路径和凭据作用域使用 Cherry 独立身份。
- Broker 校验客户端签名与进程身份，Guardian 负责遮罩和接管检测；受保护动作必须持有有效租约。取消、断连和关闭要求动作排空、重锁与保护释放确认；未确认时返回清理失败。部署前必须完成真实安装/锁屏循环验收。
- `virtualDisplay` 默认关闭，启用后仍需明确的窗口停放请求。应用停止只恢复所属窗口，失败保留恢复记录；任务退出尝试全部恢复。私有 SPI 与显示拓扑变化存在实机兼容性边界。
- 窗口恢复子进程仅通过继承管道接收请求，使用同一 app 可执行文件和当前用户权限。修改前校验密码管理器 denylist、PID、进程实例及窗口归属；恢复持有的 AX 引用，不根据复用 ID 重新查找窗口。原位置和遮挡状态仅保存在内存，拥有者断连后恢复并退出。
- 虚拟显示器由恢复进程的独立子进程创建和排列，所有继承管道禁止传播到其他新进程。必须先登记窗口再建屏、先恢复全部停放窗口再主动销毁屏幕；显示器进程退出后重新枚举确认移除。
- HID 类别覆盖不足、设备变化、启动期间输入或监听权限撤销时，不继续申请或保持解锁租约。部分设备可观察不代表 Karabiner 转发覆盖每个真实设备，完整锁屏/接管必须另行验收。
- AX/UIA/AT-SPI 密码字段不读取或输出值，Windows 的 UIA 密码标记无法确认时也不读取值；SDK 纯截图模式不遍历控件树、焦点或选区。截图本身仍可能含应用显示的信息。
- Windows 动作校验观察时的 PID、窗口句柄及元素 RuntimeId/完整身份，应用重启或窗口失效时不重新猜测目标。未确认输入不会换通道重发。

组件与配置详见[新增桌面能力](design-docs/upstream-desktop-capabilities.md)。

## Fixture Bridge 约束

- `FixtureBridge` 只用于仓库内测试夹具，不是给第三方 app 的控制平面。
- 任何面向真实 app 的能力新增，都不应该复用这条测试专用通道。

仓库级的依赖、SBOM 和 provenance 默认能力，统一写在 `docs/SUPPLY_CHAIN_SECURITY.md`。
