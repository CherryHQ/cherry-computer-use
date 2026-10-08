# AX 后台滚动

## 目标与范围

让共享 macOS 引擎通过 AXScrollToVisible 支持后台 Chromium 列表滚动，MCP 与 SDK 共用。
保留原生 AX 翻页和定向滚轮回退；不激活、不提窗口、不移动真实鼠标。
不扩展键盘、SkyLight 滚轮或 Windows/Linux。

## 验证与进度

- [x] 阅读中断会话及初版实现；确认滚轮回退仍然假报成功。
- [x] 确认权限失败来自本地与 Cherry 制品的签名身份冲突；本地构建显式固定同一证书，用户重新授权后两项权限为 granted。
- [x] 单测覆盖四个方向、一页/两页、跨区域选取、水平细条、行距异常、超大距离及无位移。
- [x] AX 翻页、揭示和定向滚轮统一要求位移证据；未知效果不报成功。
- [x] Swift 195 项（1 跳过、0 失败）、SDK 类型检查/36 项测试、原生协议 7 项通过。
- [x] 关闭 visual cursor 的无窗口 fixture smoke 通过，覆盖 9 个工具；未运行光标显示 smoke。
- [x] 同步 runtime、Cherry 开发文档和 history，记录真实验证限制。
- [ ] 后台飞书、隔离 Chrome 嵌套区域/整页、原生控件和 Cherry 消息列表：确认效果、前台激活/key window/first responder、失焦计数、窗口层级与真实鼠标不变。

## 用户要求暂缓的验收

用户正在使用电脑，要求先完成其他检查。本轮不再启动或切换测试窗口。
飞书单次内容位移已有证据，但鼠标断言失败；Chrome 的前台切换和启动竞争也未排除人工操作。
这些都不是严格验收通过，计划保持 active。

恢复时先确认测试进程启动已稳定，前台 PID 与 fixture 导出状态一致，再采集基线。
按 1 页、2 页、反向回到原位、边界和静态容器分别断言；仅清理本次创建的测试进程。
真实桌面验证与 fixture 的合成状态 smoke 分开记录。

## 决策与限制

- 没有候选不证明到边，虚拟列表可能不暴露离屏元素；无位移证据时返回失败。
- 输入可能已派发时保留 effect: possible，不把缺乏观测证据当作 effect: none。
- 指定有效容器后不因没有候选自动换到外层容器；AX 揭示自身仍可能影响外层，需要嵌套场景验收。
- 滚动距离近似；AX 缺失、几何或滚动条不提供证据时不能确认成功，包括部分窗口级目标。
- helper 构建继续使用 release 配置，并显式传 OPEN_COMPUTER_USE_CODESIGN_MODE=identity 和固定的 OPEN_COMPUTER_USE_CODESIGN_IDENTITY；不要回到自动选择证书。
- 仅本地签名并 DCO 提交，不推送。Cherry 全分支检查及生成的 computer-use 设置路由 manifest 由宿主仓库收尾记录。
