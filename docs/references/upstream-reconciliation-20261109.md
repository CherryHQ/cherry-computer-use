# 2026-11-09 上游同步评估

## 结论

有必要移植明确的正确性修复；当前不宜把上游全部功能和发布配置直接合进 Cherry。两边已经有不同的运行时与分发边界。

本次采用选择性移植。它不等同于完整 Git merge：尚未接入的上游提交仍保留分歧，不使用 `ours` 合并把这些功能伪装成已接入。

2026-11-13 更新：用户要求重新评估这些新增能力的接入价值。[后续能力评估](upstream-feature-adoption-20261113.md)建议将其纳入分批接入路线，并明确 SDK 已有能力、适配点及上游尚未完成的验证；本页记录的是第一批修复范围，不表示永久排除新功能。

## 核对基线

- Cherry `main`：`db6da8520793e210b9bd1b66e39b95a0dc02b5c0`。
- 上游 `iFurySt/open-computer-use`：`1263960a888e77297e551b560206bd44fd1b53e5`。
- 共同祖先：`386a260d1ab8b690adbbb27f7471595cf0c2b752`。
- 上游独有 75 个提交，差异涉及 448 个文件、37,790 行新增和 1,162 行删除。只读试合并报告 30 个文件冲突。
- 冲突包括原生输入与观察、权限引导、Linux 后端、SDK workspace、品牌、打包脚本和发布文档。上游仍修改 Python Linux 后端，Cherry 已用 Go AT-SPI/X11/Wayland 引擎替换它。

复核命令：

```sh
git fetch upstream
git merge-base origin/main upstream/main
git log --oneline origin/main..upstream/main
git diff --shortstat origin/main...upstream/main
git merge-tree --write-tree origin/main upstream/main
```

## 本次吸收

| 上游来源 | 解决的问题 | Cherry 适配 |
| --- | --- | --- |
| [#127](https://github.com/iFurySt/open-computer-use/pull/127) / `a432f88` | 点空白区域可能扫描整个窗口并按下远处的关闭按钮 | 限制后代候选必须包含点击点或与目标元素重叠；窗口索引不扫描内部按钮。SDK 与 CLI/MCP 共用。 |
| [#130](https://github.com/iFurySt/open-computer-use/pull/130) / `9506727` | `type_text` 使用旧焦点、把选区输入错误地追加到末尾 | CLI/MCP 读取实时 AX 焦点，优先写选区，必要时按 UTF-16 范围替换并恢复光标；写入失败不换通道重发。SDK 已使用 CDP/原生按键，保留其路径。 |
| [#111](https://github.com/iFurySt/open-computer-use/pull/111) / `7657d49` | 控件忽略或归一化写入却返回成功；Windows/Linux CLI 丢失首尾空白 | macOS/Windows 写一次后最多读回 0.6 秒，不确认则返回错误；SDK 保持可能已执行的错误语义。允许清空文本，按字面保留空白。Linux Go 引擎已有读回，本轮只修 CLI 的字面参数传递。 |
| [#154](https://github.com/iFurySt/open-computer-use/pull/154) / `7c40742` | 非有限 AX 坐标导致整数转换崩溃，截图与窗口几何不一致 | 校验 AX/窗口坐标及面积，安全渲染异常值；SCK 使用 CG bounds 并核对图片比例。Cherry 没有上游硬件截图路径，不引入该路径。 |
| [#129](https://github.com/iFurySt/open-computer-use/pull/129) / `febb387` | 长连接的 autorelease 对象累积至断开 | CLI/MCP proxy、app-agent 请求与 tool dispatcher 按请求释放临时对象。Cherry 没有上游的 idle socket timer，不引入它。 |
| [#119](https://github.com/iFurySt/open-computer-use/pull/119) / `ec85d35` | 代理进程从不同工作目录读取相对调用文件 | 在调用方解析 `--args-file` / `--calls-file`；JSON 参数值保持不变。 |
| [#147](https://github.com/iFurySt/open-computer-use/pull/147) / `4dbaa7a` 的截图部分 | Windows CLI 截到覆盖目标的其他应用 | 改为对已解析的 HWND 调用 PrintWindow，拒绝无效、隐藏、最小化或已移动的窗口几何，失败不截取桌面。SDK 原有目标窗口截图与 Cherry 宿主输入路由保留。 |

## 已有等效行为或需要后续适配

- **无需重复移植**：[#126](https://github.com/iFurySt/open-computer-use/pull/126) 的 Retina hit test，Cherry 已在 `fd2abb8` 修复并有 SDK 回归测试；Linux 的键位、焦点与写值问题应在当前 Go 引擎核对，不能恢复 Python 后端；Windows SDK 已用 PrintWindow。
- **值得单独接入**：[#118](https://github.com/iFurySt/open-computer-use/pull/118) 的 focused sheet 观察依赖精确窗口绑定，不能只允许 AXSheet 根节点；[#145](https://github.com/iFurySt/open-computer-use/pull/145) 的可见节点预算、[#140](https://github.com/iFurySt/open-computer-use/pull/140) 的批量 AX 读取、光标跟随实时窗口、Windows DPI/滚动/经典控件子窗口点击都有价值，但涉及与 Cherry 现有语义的进一步整合。
- **按产品需求选择**：JS REPL/CLI、DeepSeek Harness 集成、`query` / `select_text`、选择性截图与 AX 观察、持久配置和 WebP 输出提供新能力；Cherry 的宿主接入主要依赖 SDK，应先定义 SDK 的对应契约。
- **需要明确产品设计**：后台输入扩展、虚拟显示器和 Locked Use 带来新的窗口放置、遮屏与本地输入隔离、broker/Guardian 生命周期和签名要求，不属于必须同步的 bug fix。
- **保留 Cherry 自己的实现**：Changesets、六个平台包、品牌和 Bundle ID、SDK 协议 v2、CDP、Linux Go/Wayland 后端、第三方声明。上游版本号及发布记录不能作为 Cherry 已发布的能力。

## 验证边界

Swift 单测、独立 AppKit 窗口的点击/文本/写值回归、现有 fixture smoke、SDK 测试、Go 测试与 PowerShell 函数测试分别验证不同层次。实际执行结果见 [history](../histories/2026-11/20261109-0542-upstream-reconciliation.md)。

当前机器未授予录屏权限，未验收真实 SCK 截图；Windows 的函数测试使用模拟 UIA pattern / Win32 状态，尚未执行真实 Windows 桌面截图验收。不得把这些边界写成三平台完整桌面验收。
