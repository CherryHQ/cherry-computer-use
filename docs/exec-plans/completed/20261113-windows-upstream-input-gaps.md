# Windows 上游输入修复补齐

## 目标与范围

用户要求核对 Windows/Linux 上游改动是否已吸收。核对已提交的按需观察、树预算、密码保护和输入身份修复后，补齐 Windows 尚未适配的物理 DPI 坐标、滚轮消息坐标/方向与经典子控件点击。保留 Cherry 的 HWND/UWP/CDP 路由，不引入上游 Python Linux 后端或额外全局输入模式。

## 来源和约束

- 固定上游 `5136415`（DPI/滚动）与 `4dbaa7a`（目标窗口截图/子控件点击）；后者的截图部分已吸收。
- SDK 与 CLI/MCP 共享 PowerShell 引擎，修复须同时覆盖两条入口。
- 当前 macOS 只能执行 PowerShell 生产函数及模拟 Win32 测试、Go 回归和交叉编译；Windows 混合 DPI/真实控件验收独立记录。
- 不运行会切换当前 macOS 桌面的验收。

## 实施与验证

- [x] 对照固定上游提交与 Cherry 当前源码确认遗漏。
- [x] 物理坐标初始化、正确的滚轮屏幕坐标与左右方向、限定目标窗口内的子控件命中。
- [x] 生产函数回归、既有 Windows/Linux Go 验证及 Windows x64/arm64 编译。
- [x] 同步 history、发布说明与现有 PR 所需的接入/验收记录。

## 决策记录

- 2026-11-13：上游的鼠标全局兜底不直接导入。保留 Cherry 现有宿主应用路由，仅修复坐标和目标校验；经典控件坐标点击必须投递到实际子 HWND，并复核几何和进程归属。
- 2026-11-13：同步修复已有宿主应用指针路径使用主屏尺寸归一化的问题，改为 virtual desktop 原点/尺寸及相应标志；不会增加新的全局输入入口。
- 2026-11-13：Windows Go、Linux/共享 Go race、Windows amd64/arm64 编译与八组 PowerShell 生产函数回归通过。新回归直接运行生产 C# 命中/坐标方法，Win32 边界使用可控模型；真实 Windows 混合 DPI 与控件验收仍待完成。

## 参考

- [WM_MOUSEWHEEL 的屏幕坐标](https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-mousewheel)
- [WM_MOUSEHWHEEL 的正负方向](https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-mousehwheel)
- [线程 DPI awareness](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setthreaddpiawarenesscontext)
