# Cherry Computer Use SDK 与 npm 分发

## 目标

把当前以 CLI/MCP 为入口的项目改成可被 Cherry Studio 主进程直接使用的 TypeScript SDK，并通过 npm 分发配套原生 runtime。第一阶段从原有普通工具入口接入 SDK；持久 JavaScript / REPL 属于后续可选 Code Mode 增强，关闭 Code Mode 时继续按原方案执行。macOS、Windows、Linux 都是交付目标；Linux 的 X11 与 Wayland 分别验收。本地源码联调与正式安装使用相同的 SDK 公共 API。

状态：SDK 客户端、三端生命周期和首个桌面切片已实现。Windows 11 ARM64/Linux X11 已通过真实 SDK 观察、截图与语义点击；macOS GUI 待系统授权，生命周期已验证。Cherry 已接入本地权限引导；SDK/原生每应用控制已接通；Cherry 控制归属/用户停止状态、Tray 停止、SDK 软件光标、其余动作、Wayland、平台包、Agent 工具接入与发布尚未完成。

## 范围

- 包含：SDK 公共接口、结构化原生协议、进程生命周期、按平台拆包、发布流水线、本地开发入口及 Electron 制品验证，以及宿主集成所需的每应用控制会话、Tray 手动停止与软件光标。
- 不包含：模型调用循环、Agent 历史与上下文管理、Cherry 的业务授权 UI，以及第一阶段的持久 REPL 和其他语言 SDK。
- 保留现有 Swift/Go 平台实现，先调整公共边界与分发，不同时重写三端引擎。CLI/MCP 可以继续作为相同底层能力的适配入口。

## 背景

调研基线为 fork 的 `386a260d1ab8b690adbbb27f7471595cf0c2b752`（上游 `0.3.5`）。

- [当前架构](../../ARCHITECTURE.md)：macOS 使用 Swift 库和带权限身份的 app agent；Windows 使用 Go/PowerShell UIA；Linux 使用 Go/Python AT-SPI。
- [npm 构建脚本](../../../scripts/npm/build-packages.mjs)：当前生成三个 CLI 别名包，每个都包含所有平台的二进制，没有可导入的 TypeScript SDK。
- [此前的拆包计划](20260423-cross-platform-npm-distribution.md)：上游曾采用平台包与 `optionalDependencies`，因新增包名的 npm 发布权限受阻，改回三个已有包内置全部 runtime。fork 需要重新配置发布权限，不能只改包结构。
- [嵌入式 app agent 隔离](20260901-embedded-app-agent-socket-isolation.md)：已有可选 socket namespace，可作为宿主隔离的基础，但不等于已经具备完整的 SDK 会话关闭协议。
- [发布脚本](../../../scripts/npm/publish-packages.mjs)仍面向上游包名与目录发布；版本源来自插件 manifest，需调整为 SDK/runtime 共用的发布版本源。

## SDK 边界

详细接口与首个实现切片见 [packages/sdk 设计草案](../../design-docs/computer-use-sdk.md)。

三端的原生改动见 [运行时设计](../../design-docs/computer-use-runtime.md)：私有进程归属、独立控制读取、动作/观察分离和清理确认。Wayland 的应用窗口与 portal 画面映射在原生生命周期切片期间先做实验；若现有目标模型不足，先调整公共契约。

选型方向：Windows/Linux 沿用 Go 承担协议、会话与请求调度，macOS 继续使用 Swift。Linux 优先通过真实原型验证 Go 直接接入系统 API；Python 是复用上游的过渡选项，最终后端语言和进程布局待原型确定。独立 helper 与 Electron ABI 解耦的收益不归属于 Go 特有能力，原生绑定的构建和系统库依赖需要分别验收。

建议链路为：Cherry 主进程服务 → TypeScript SDK → 本地原生协议 → Swift/Go runtime → 操作系统。

SDK 是 Node 库，不依赖 Electron、模型 SDK 或 Cherry 内部模块。它负责 runtime 路径解析、显式启动、协议握手、请求与事件、取消和关闭。Cherry 决定何时启用、允许操作哪些目标、如何显示授权与执行状态。原生端负责平台权限、目标解析、输入执行与实际资源清理。

第一阶段保留独立 helper 进程及 macOS `.app` 身份，不在 Node 侧引入原生扩展、FFI 或 Electron ABI 重编译链；helper 内部是否需要原生库绑定由平台原型验证。

建议接口形状：

```ts
const computer = await ComputerUse.start({ runtimePath })
const capabilities = await computer.getCapabilities()
const permissions = await computer.getPermissionStatus()
const apps = await computer.listApps()
const session = await computer.openAppSession({ appId }, { signal })
const state = await computer.getAppState({ appSessionId: session.id }, { signal })
await computer.act({ type: 'click', appSessionId: session.id, snapshotId: state.id, elementId }, { signal })
await computer.close()
```

`runtimePath` 为可选的显式路径，用于本地构建和 Electron 外置资源；省略时解析已安装的平台包。上述签名仅表达边界，正式字段在实现协议时确定。

首批契约：

1. `import` 无启动或授权副作用；启动与权限状态查询不自动打开引导窗口。操作系统权限请求单独显式触发，并返回是否需要系统交互。
2. 原生 service 返回结构化数据，再由 CLI/MCP 格式化输出。SDK 不解析 MCP 的展示文本，也不把每个操作变成一次独立 CLI 调用。
3. Snapshot 明确目标、窗口、元素引用、截图尺寸及坐标转换信息。动作引用所属 snapshot；过期或跨目标引用被拒绝。截图缩放后坐标仍能准确映射。
4. 能力按当前平台与桌面会话报告，包括截图、语义操作、坐标输入及系统依赖；安装了 Linux 包不等于 Wayland 的所有功能可用。
5. 请求有 ID、结构化错误和取消语义。取消必须送达执行端并清理本次输入状态；不能只让 JS Promise 提前结束。已发生的点击不能回滚，取消结果需表达动作是否已开始或结果未知。
6. 桌面动作串行执行，取消控制消息不能被正在执行的动作阻塞。版本握手在动作前拒绝不兼容协议。
7. `close()` 释放本客户端所属请求、输入、overlay 和 helper 资源；macOS 必须覆盖 LaunchServices app agent，不只结束 CLI proxy，也不退出其他宿主的 agent。

在仓库内维护一份版本化的协议 schema 和跨语言 fixture。可以复用 JSON-RPC 的请求关联与错误结构，不把 MCP tool schema 作为 SDK 领域模型。第一阶段不单独发布 protocol npm 包。

拟新增 `packages/sdk/`（源码、导出、类型、测试、README）和 `protocol/`（schema、fixture）。现有 `packages/OpenComputerUseKit/`、`apps/OpenComputerUse/`、`apps/OpenComputerUseWindows/`、`apps/OpenComputerUseLinux/` 逐步接入，避免先做大规模目录搬迁。

## npm 包结构

以下使用 `@cherrystudio` 作为拟定 scope，首次发布前验证组织权限与包名可用性。

| 包 | 内容 |
| --- | --- |
| `@cherrystudio/computer-use` | JS SDK、类型声明、平台解析逻辑；不内置二进制 |
| `@cherrystudio/computer-use-darwin-arm64` / `darwin-x64` | 对应 macOS runtime，保留完整 `.app` bundle |
| `@cherrystudio/computer-use-win32-arm64` / `win32-x64` | 对应 Windows runtime 与所需资源 |
| `@cherrystudio/computer-use-linux-arm64` / `linux-x64` | 对应 Linux runtime 与所需资源 |

- 平台包用 `os` / `cpu` 限制安装范围；SDK 通过精确版本的 `optionalDependencies` 引用六个平台包。初期 SDK 与 runtime 统一发布版本，另有独立的协议版本号。
- [npm 允许 optional dependency 缺失](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/#optionaldependencies)，因此 SDK 必须报告可识别的 runtime 缺失错误，不能静默找 PATH 中的其他版本或启动时联网安装。
- SDK 产出编译后的 JS、类型声明与明确的 `exports`；通过干净消费者验证 ESM/CJS 加载和 Cherry 实际主进程构建路径。消费者不需要编译 TypeScript、Swift 或 Go。
- 平台包由现有构建脚本生成到 `dist/`，不手工维护六份重复源码。打包目标必须显式传递，不能用构建机器的 `process.arch` 代替目标架构。
- 第一阶段可保留现有 universal macOS app 构建；若两个 Darwin 包复用同一 universal app，应在 manifest 中如实记录。保持签名后的 bundle 完整性。
- npm 安装不编译原生代码、不修改 MCP/插件配置；系统权限引导属于显式运行操作。Linux 按实际后端探测并记录 AT-SPI、桌面 portal 及原生库依赖；只有采用 Python 过渡方案时才要求 Python/GI，不能声称 npm 已包含所有系统依赖。
- 发布文件使用 allowlist：运行代码、类型、必要资源、许可证与说明。保留第三方 notices，替换上游提取的官方 cursor 图形及 Cherry 不拥有的品牌资源。

## 发布流水线

在现有 [release workflow](../../../.github/workflows/release.yml)、[打包入口](../../../scripts/release-package.sh)和 npm 脚本上改造，不并存两套相互独立的发布版本。

1. 根私有 `package.json` 增加统一发布版本；SDK、平台包、native version 与保留的插件入口从同一来源生成或校验。插件 manifest 不再独自决定 SDK 发布版本。
2. fork 的 npm 发布 allowlist 限于 Cherry 包名，更新 `repository`、文档和 workflow 配置；默认不向原上游三个 npm 包发布。
3. 构建、签名、打包后形成固定 `.tgz` 制品及摘要，验证这些 tarball，最后发布同一批字节；发布阶段不重新构建或重新打包。
4. 先发布并确认平台包，再发布精确依赖它们的 SDK。首次使用 prerelease 和 `next`；全部制品与验收完成后再推进稳定版本。
5. 多包发布失败可续跑，但“同名版本已存在”不足以判定成功，必须比较 registry integrity 与待发布 tarball。SDK 尚未发布时，已发布的平台包可保留等待续跑。
6. 为每个新包配置 [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/)，绑定 Cherry 仓库与具体 workflow，使用支持的托管 runner 发布 provenance。npm 首次建包权限、签名身份均是发布前需实际核验的条件；本计划不代表已完成配置。

## 本地联调与 submodule

首选已链接的两个独立仓库工作区：在本仓库构建/监听 SDK，Cherry 通过项目局部链接使用其构建产物，并显式提供本地 runtime 路径。共享代码始终从 `@cherrystudio/computer-use` 导入，不跨仓库引用内部 TypeScript 源码。

正式依赖保存 npm 版本。本地路径放在开发者配置中，不把机器路径写进提交的 manifest/lockfile；联调辅助脚本需可恢复正式依赖，并依据 Cherry 固定的 pnpm 版本验证实际链接行为。提交前再用 `npm pack` tarball 在干净消费者中验收，避免链接掩盖漏文件和错误导出。

submodule 可以作为以后锁定源码 commit 的选项，但它本身不解决 SDK 构建、依赖安装和二进制分发。当前 Conductor 使用多个 Git worktree，[Git 官方文档](https://git-scm.com/docs/git-worktree#_bugs)仍说明 submodule 支持不完整，因此第一阶段不默认添加。

如果采用 submodule，先用临时仓库验证两个 worktree 的创建、初始化、切换版本和归档；通过后再落 `.gitmodules` 与固定 gitlink。检出使用记录的 commit，不在 setup 中自动跟踪远端最新分支。仅将 SDK 包纳入消费关系，正式制品仍验收 npm 依赖路径。

## 里程碑与验证

### 1. 先做 SDK 的真实最小闭环

- 加入 SDK 包与协议 schema，提取结构化 service 返回值，三端实现握手、能力与权限查询、观察、至少一个真实动作、取消和关闭。
- 验证：干净 Node 消费者直接导入；三端协议 fixture 一致；重复启动/关闭无本客户端资源残留；取消后不会继续执行排队动作；过期 snapshot 不会点错目标。
- 后续将当前其余动作接入相同契约。第一个动作通过只证明切片成立，不代表九个 tools 或三端已经交付完整。

### 2. 每应用控制与宿主停止入口（下一阶段）

资源归属和停止契约以 [runtime 设计](../../design-docs/computer-use-runtime.md)为准。按以下顺序推进，每一步分别验收：

| 顺序 | 改动 | 验证 |
| --- | --- | --- |
| 1 | macOS 固定 helper bundle 与签名身份；本地联调使用已有证书，避免强制 ad-hoc | 用户重新授权后，重建/重启仍满足授权记录的代码要求，fresh helper 返回真实权限并完成桌面 fixture |
| 2 | 在任务级 SDK/runtime 内增加每应用控制会话及 schema | 双应用状态隔离；应用重启、会话关闭后的旧引用拒绝；跨任务控制同一应用得到协调 |
| 3 | Cherry Tray 展示受控应用和任务，提供单应用/全部停止 | 停止贯通原生；无排队动作继续执行、输入或光标残留；不退出目标应用；用户停止不能被 Agent 自动恢复 |
| 4 | 将软件光标可变状态改为每应用持有，先接已有点击，再接普通工具 | 两应用光标互不覆盖，窗口跟踪正确；动画不代替动作结果；Code Mode 关闭时可用 |
| 5 | 扩展输入、移动、拖拽反馈及原生动作/分步取消 | 真实桌面效果、按键/按钮释放及全局输入协调；macOS、Windows、Linux X11/Wayland 分别报告能力 |

已采用固定证书重建 macOS helper；重新授权与权限保持验证待用户完成。在等待授权期间，已推进不依赖该授权的 SDK/原生应用会话与停止契约；宿主控制归属、Tray 和光标仍未接入。后续 Code Mode 复用相同的控制和停止边界，不成为本阶段前提。

### 3. 拆 npm 包并验证安装产物

- 改造已有 staging/publish 脚本、统一版本源和目标平台 manifest，产出 SDK 与六个平台 tarball。
- 验证：从 tarball 安装到无源码目录的消费者；核对 exports、类型、可执行权限、runtime 资源及版本握手；覆盖 optional 包缺失和错误架构。检查 SDK 包没有全平台二进制和插件安装器。
- 构建全部架构与实际执行分开报告；交叉编译成功不能代替目标系统运行验证。

### 4. Cherry 联调与三端交付

- Cherry 主进程服务管理 SDK，向 renderer 提供既有 IPC 范式的命令与状态；SDK 不进入 renderer。
- 将 native runtime 放在 Electron 可执行的真实文件路径，验证 ASAR 外资源、安装路径含空格及目标架构选择。macOS 保留实际执行后端的独立 helper bundle 身份，纳入 Cherry 的签名/公证链；Windows 验证签名与交互桌面；Linux 验证系统依赖。
- 验证：开发环境与已打包 Cherry 均完成打开目标、观察、操作、取消和退出；所有对外宣称支持的动作均在目标系统验收。
- Agent 验证：关闭 Code Mode，通过原有普通工具完成观察、真实图片返回及下一次调用的动作执行；同一控制任务复用 SDK 实例，停止后原生资源得到释放。此验收不依赖持久 JS 执行器。
- Linux X11、GNOME Wayland、KDE Wayland 分别测试截图与坐标输入。上游 Wayland 的 best-effort 行为需要补齐 portal/PipeWire/输入注入路径；不能用隐藏能力长期替代用户要求的三端交付。

## 风险

- 原生服务仍返回 MCP 展示内容：必须先在引擎边界提取结构化结果，不能让 SDK 解析文本形成第二套状态。
- macOS agent 的生命周期与进程内 snapshot 状态：启动隔离、取消和关闭要覆盖真正持有资源的进程。
- 包权限、平台签名、Linux 桌面能力：分别验证，不以 npm 发布成功代替平台可用性。
- 包拆分与源码联调容易掩盖目标平台文件缺失：以 tarball 和已打包 Electron 应用作为最终消费证据。

## 进度记录

- [x] 检查 fork 基线、现有 npm staging/release 与三端架构。
- [x] 确认上游退回 bundled 包的原因与当前 worktree/submodule 约束。
- [x] 形成 SDK、npm 与本地联调方案。
- [x] 细化 SDK 目录、公共 API、快照身份、动作结果与原生取消/关闭契约。
- [x] 明确 Agent 编写脚本的入口，检查 Cherry 现有 code-mode 的持久状态、图片输出和授权缺口。
- [x] 确认持久 Code Mode 延后；关闭时保留普通工具路径，第一阶段 SDK 不依赖 REPL。
- [x] 实现 SDK 客户端、协议 schema/生成器、进程生命周期与结构化 API；验证 SDK ESM/CJS tarball 和类型声明。
- [x] 添加 macOS/Windows/Linux 的 SDK CI 配置；仅在本机运行过测试，远端矩阵结果待验证。
- [x] 形成三端原生运行时设计提案，记录进程归属、取消/关闭、桥接改动与 Wayland 边界实验。
- [x] 记录 Go 的职责、Python 的过渡定位与后端选型的验证条件。
- [x] 实现三端 `serve --stdio` 生命周期入口、Swift/Go 取消与关闭，验证 macOS/Linux 原生进程隔离和退出；Windows 两种架构交叉编译通过。
- [x] Linux Go 原型在无 Python 的 Xvfb/GTK 环境直接读取 AT-SPI、操作计数按钮并捕获 X11 窗口；此原型不接入 SDK。
- [x] 在 Parallels Windows 11 ARM64 验证原生生命周期、真实 SDK 桌面操作及 worker 取消/宿主退出清理。
- [ ] 验证远端三系统 CI。
- [x] 将 Go AT-SPI/X11 接入 SDK，在无 Python 环境验证结构化观察、语义点击、窗口映射/截图及正常关闭。
- [ ] 完成 Wayland 后端实验与分步输入取消/恢复验收，再确定完整 Linux 后端布局。
- [x] 实现三端结构化 `listApps → getAppState → 单次元素 click`，验证快照归属、失效及动作/观察结果分离。
- [x] 使用本机固定证书重建 macOS helper 并验证签名；未把本机身份写入源码。
- [ ] 重新授权后验证 macOS helper 重建/重启不会使授权失配。
- [ ] 授予 macOS app agent 系统权限并完成真实 GUI 验收。
- [x] 接通 macOS 显式权限申请与 Cherry 本地 SDK 权限入口；开发链接和原生 helper 路径保留在项目 `.context` 下。
- [x] 记录任务级与每应用控制会话、Tray 停止、软件光标的归属与验收设计；原生上下文已落地，宿主与光标待实现。
- [x] 实现每应用控制上下文、SDK/schema v2 和三端停止契约；Swift/Go 阻塞/满队列取消与隔离测试通过。
- [x] 使用新协议重跑 Linux X11 真实点击、应用停止及第二 runtime 继续操作。
- [ ] 恢复 Parallels 命令通道后重跑 Windows 应用会话桌面测试；本轮 ARM64 构建已通过。
- [ ] 实现 Cherry Tray 单应用/全部停止，验证原生清理及阻止 Agent 自动恢复。
- [ ] 实现每应用软件光标，先接已支持点击，再扩展输入/移动/拖拽。
- [ ] 补齐其他动作、权限流程和平台限制的原生验收。
- [ ] 完成平台 tarball 与安装产物测试。
- [ ] 完成 Cherry 联调与三端桌面验收。
- [ ] 配置并验证 npm 发布权限、签名身份和正式发布链。

## 决策记录

- 2026-09-16：建议先提取 SDK/原生协议，再拆分 npm 包；保留三端既有实现和独立 helper，不以 MCP 外包装充当深度集成。
- 2026-09-16：建议以已链接的独立工作区联调，正式分发走 npm；submodule 在 worktree 生命周期验证通过后再决定是否采用。
- 2026-09-16：本轮只落调研方案，不修改 runtime、Cherry 依赖、submodule 或发布配置。
- 2026-09-16：根据用户最终确认，将持久 JavaScript 脚本调用列为后续可选 Code Mode 能力，不属于第一阶段；关闭 Code Mode 时继续走普通工具 → Cherry 主进程服务 → SDK。后续复用现有 `meta` / `codeMode` 核心，SDK 不实现解释器。
- 2026-09-16：按用户要求先实现 SDK 部分，拆出可独立检查的客户端与协议契约；原生 `serve --stdio`、平台发布和 Cherry 接入继续保留为未完成项。客户端通过 fixture 不等于三端原生验收通过。
- 2026-09-17：继续设计运行时，建议 macOS 使用任务私有 app agent、Windows 保留按请求 PowerShell bridge、Linux 以持续存活的 Python worker 承载任务后端。先完成真实原生进程闭环，同时验证 Wayland 目标/坐标模型；本轮仅更新设计文档。
- 2026-09-17：根据后续讨论修订 Linux 初始提案：Go 适合协议/会话/调度，Python 仅为过渡复用选项。先验证 Go 直接接入 Linux 系统 API 的功能、清理及分发成本，再确定后端语言和进程布局；Wayland 会话持续存活不要求 Python。此轮仅记录设计决策，未实现原型。
- 2026-09-17：按用户要求开始 runtime 实现，完成生命周期切片及 Go AT-SPI/X11 独立原型。尚未接入的 SDK 桌面能力明确报告 unsupported，Windows/Linux 的 SDK 路径不启动旧脚本 bridge；Wayland、输入恢复和最终后端选型继续保留为待验证项。

- 2026-09-17：继续实现结构化观察/语义点击。Linux SDK 采用 Go 进程内 AT-SPI/X11，Windows 按请求 PowerShell worker 由私有 Job Object 管理，macOS 在私有 app agent 中持有 AX 引用。Windows ARM64 与 Linux X11 的真实 SDK 闭环通过；macOS GUI 等待明确系统授权。对 uncertain 动作禁用后续桌面调用，保持无自动重放。未改 Cherry、submodule 或发布配置。

- 2026-09-18：调整推进顺序为本地 SDK 接入 Cherry → 系统权限查询/申请/返回刷新 → 原生桌面验收 → npm 分发。权限页使用短会话，每次刷新启动新 helper 读取系统权限；控制任务以后仍使用独立长会话。尚未发布 npm，Cherry 暂用 `.context` 相对链接进行联调，不能据此宣称干净 CI 或正式打包交付完成。

- 2026-09-18：将已有原生拖拽授权窗口接入 SDK；权限请求保持 pending，完成/关闭/取消后才释放窗口资源。SDK 默认给予 5 分钟交互预算；SDK 模式禁用旧 app 自行退出/重启，进程生命周期仍由宿主管理。

- 2026-09-18：确认任务级 SDK/runtime 内的每应用控制会话模型；Cherry 负责 Tray、控制归属和用户停止状态，runtime 负责原生停止与每应用光标。先固定签名并完成 macOS 验收，再推进应用会话 → Tray 停止 → 点击反馈 → 输入/移动/拖拽，Code Mode 继续延后。本轮只更新文档。

- 2026-09-18 实现轮：原生应用会话与停止契约进入 v2，旧无作用域调用不再接受；先提供可验证的原生停止，Cherry 用户停止状态/跨任务归属和 Tray 接入作为下一切片。固定签名已完成，系统重新授权仍待用户；不把本机签名成功当作 TCC 授权保持已验证。
