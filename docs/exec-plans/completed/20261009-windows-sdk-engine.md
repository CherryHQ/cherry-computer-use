# Windows SDK 复用原有引擎

## 目标与范围

修复 Cherry Studio #21405：Windows SDK 使用已有 Windows 引擎的树遍历与七种动作，保留任务私有会话、快照身份、取消和不使用全局输入的边界。修复 stdin UTF-8 解码与解析失败误报不确定副作用。

不包含发布、Cherry Studio 依赖升级、Linux 动作扩展、多窗口选择或 GPU 窗口截图策略重做。SDK 保留窗口定向截图，不能用屏幕区域截图替代后台窗口图像。

## 实施与验证

1. 共享 Go 会话增加可选完整动作驱动，按现有协议严格验证输入；验证会话隔离、失效快照、已完成动作与未知副作用。
2. Windows 引擎拆出可复用动作函数；SDK 复用树记录与动作，仅处理身份和协议。验证中文、坏 JSON、七种动作、取消和 worker 归属。
3. 运行 Go、SDK、Windows 构建与可用的虚拟机测试；记录桌面验证与未覆盖场景，更新文档和 history。

## 决策

- 保留 Linux 单次语义点击驱动，不顺带改变其平台行为。
- 不确定副作用仍然关闭桌面操作；只有确认尚未执行的失败才返回 effect=none。
- 原有 Win32 定向消息可能不被应用处理；接入能力不意味着所有应用都接受后台输入。

## 进度

- [x] 源码与协议核对。
- [x] 实现和回归测试。
- [x] 验证、文档和 history。

## 验证结果

- 共享 Go race 测试、Windows/Linux Go 测试、Go vet（含 Windows 目标）通过。
- Windows ARM64/x64 编译通过；Windows 11 ARM64 虚拟机运行 Go 会话与 bridge 测试通过。worker 首次冷启动超过原有 10 秒等待，定向重跑及最终整组重跑通过。
- SDK 类型检查、构建、44 项客户端/打包测试通过。
- Windows 原生协议 4 项通过、3 项 macOS 专属跳过；原有桌面 fixture 和新增七动作 fixture 均通过。新增用例约 97 秒，覆盖中文截断显示后的点击、辅助动作、Unicode value/text、按键、滚动、拖拽、多次坐标点击与清空值。
- 虚拟机使用显式 runtimePath 指向本地 ARM64 helper，通过 Electron 的 Node 24.16 模式运行 SDK；未替换已安装的 Cherry Studio。PowerShell 调用 GUI 子系统 Electron 时必须等待进程结束再清理 fixture。
- 文档骨架、changeset 选择与 diff 空白检查通过。通过单独 PR 交付；本轮不包含 npm 发布。

## 保留边界

窗口选择仍沿用 MainWindowHandle；GPU 窗口可能返回空白图像。定向消息是否被第三方应用处理依赖应用实现。fixture 不能替代真实应用、不抢焦点/不动鼠标验收；本轮未声明这些场景通过。
