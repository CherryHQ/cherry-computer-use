# 设计文档索引

用这个目录集中管理架构设计和产品设计文档。

建议约定：

- 一个主题一份文档。
- 每份文档写清当前状态和简短摘要。
- 关联引入它的 execution plan 或 spec。

## 初始文档

- `core-beliefs.md`
- [packages/sdk 设计草案](computer-use-sdk.md)：客户端、三端生命周期与首个桌面切片已实现，剩余能力与平台发布待接入；Code Mode 为后续可选入口，第一阶段使用普通工具接入。
- [原生运行时设计](computer-use-runtime.md)：Windows ARM64/Linux X11 已通过 SDK 桌面测试；记录私有进程归属、语义点击、macOS 授权与 Wayland 的待验证边界。
