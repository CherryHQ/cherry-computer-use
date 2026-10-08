# Cherry Computer Use

[English](README.md) · [简体中文](README.zh-CN.md)

Cherry Studio 的本地桌面自动化 runtime、MCP CLI 和 TypeScript SDK，支持 macOS、Linux 和 Windows。

项目 fork 自 [iFurySt/open-codex-computer-use](https://github.com/iFurySt/open-codex-computer-use)，保留上游署名、MIT license 和第三方声明。下方演示为上游 runtime 的演示，Cherry 版本使用独立品牌和发布身份。

`@cherrystudio/computer-use`（SDK）已发布；0.1.0 需要显式指定原生 `runtimePath`，后端分发补丁通过六个 OS/CPU 平台包支持自动安装与发现。CLI workspace 保持 private，不公开发布。参见 [SDK 使用说明](packages/sdk/README.md)和[发布指南](docs/releases/RELEASE_GUIDE.md)。

## 演示

### Codex App 和 Codex CLI

[![open-computer-use 自定义演示封面](./docs/generated/readme-assets/open-computer-use-demo-cover.png)](https://youtu.be/2s6aVpGiwaQ)

<sub><em>`open-computer-use` 作为 Computer Use，在 Codex App 和 Codex CLI 里使用，和官方体验一致。</em></sub>

### Gemini CLI

https://github.com/user-attachments/assets/eacb3b15-f939-46c7-b3b3-6f876977a58d

<sub><em>Gemini CLI 通过MCP接入使用 `open-computer-use`，实现完整的 Computer Use 操作。</em></sub>

### Linux

https://github.com/user-attachments/assets/e036b1c8-2200-4896-abd4-19225915cf66

<sub><em>`open-computer-use` 在 Linux 里使用</em></sub>

## Quick Start

CLI 暂不发布。可在 macOS 执行 `OPEN_COMPUTER_USE_CODESIGN_MODE=adhoc npm run npm:pack` 生成本地 tarball，再安装生成的文件：

```bash
npm i -g /path/to/cherrystudio-computer-use-cli-<version>.tgz
```

通过 npm 安装后也会同时提供短命令 `ocu`。

> [!IMPORTANT]
> macOS 运行环境要求 macOS 14.0 或更高版本。

**macOS 第一次使用前，需要授权 `Accessibility` 和 `Screen Recording` 的权限，windows和linux无需执行**

```bash
open-computer-use
# 或
ocu
```

开始用前可以通过一键安装到主流的Agent里：
```bash
# 一键安装到 Codex，写到 ~/.codex/config.toml 中
open-computer-use install-codex-mcp
ocu install-codex-mcp
```

也可以手动配置到你自己的客户端里：

```json
{
  "mcpServers": {
    "open-computer-use": {
      "command": "open-computer-use",
      "args": ["mcp"]
    }
  }
}
```

### Skill

一键安装skill：

```bash
# 安装到 Codex
npx skills add CherryHQ/cherry-computer-use -g -a codex --skill open-computer-use -y
npx skills ls -g -a codex | rg 'open-computer-use'
```

安装到 Claude Code
```
npx skills add CherryHQ/cherry-computer-use -g -a claude-code --skill open-computer-use -y
```

更新已有的全局安装，包括上面安装到 Codex 的那份：

```bash
npx skills update open-computer-use -g -y
```

也可以手动下载 [`open-computer-use` skill](./skills/open-computer-use) 安装

## 更多

除了直接用上面的 MCP JSON 配置，你也可以用一些内置子命令：

```bash
# 一键安装到 Codex，写到 ~/.codex/config.toml 中
open-computer-use install-codex-mcp

# 一键安装到 Codex 插件，主要方便在 Codex App 中使用
open-computer-use install-codex-plugin

# 一键安装到 Claude Code，写到 ~/.claude.json 中
open-computer-use install-claude-mcp

# 一键安装到 Gemini CLI 当前项目，写到 ./.gemini/settings.json
open-computer-use install-gemini-mcp

# 一键安装到 Gemini CLI 用户级配置
open-computer-use install-gemini-mcp --scope user

# 一键安装到 opencode，写到 ~/.config/opencode/opencode.json（或当前生效的配置文件）
open-computer-use install-opencode-mcp

# 直接调用单个 Computer Use tool，输出 MCP 风格的 JSON result
open-computer-use call list_apps
ocu call list_apps
open-computer-use call get_app_state --args '{"app":"TextEdit"}'

# 在同一个进程里编排连续动作，复用 get_app_state 拿到的 element_index
# 连续动作默认会在成功的相邻操作之间 sleep 1 秒
open-computer-use call --calls '[{"tool":"get_app_state","args":{"app":"TextEdit"}},{"tool":"press_key","args":{"app":"TextEdit","key":"Return"}}]'
open-computer-use call --calls-file examples/textedit-overlay-seq.json --sleep 0.5

# 检查权限；只有缺失时才会拉起引导，已全部授权则只打印状态并退出
open-computer-use doctor

# 查看帮助
open-computer-use -h
ocu -h
```

## Cursor Motion

Cursor Motion 是一个面向 macOS 的开源光标运动系统，基于 Software.Inc 几位大佬的公开信息实现的开源版本，也可以到 [Releases 页面](https://github.com/iFurySt/open-codex-computer-use/releases) 下载 app 运行。

[![Cursor Motion 自定义演示封面](./docs/generated/readme-assets/cursor-motion-demo-cover.png)](https://youtu.be/KRUq5GUHv1Q)

## Star History

<a href="https://www.star-history.com/?repos=iFurySt%2Fopen-codex-computer-use&type=date&legend=top-left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=ifuryst/open-codex-computer-use&type=date&theme=dark&legend=top-left" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=ifuryst/open-codex-computer-use&type=date&legend=top-left" />
    <img alt="open-computer-use Star History 趋势图" src="https://api.star-history.com/chart?repos=ifuryst/open-codex-computer-use&type=date&legend=top-left" />
  </picture>
</a>

## License

[MIT](./LICENSE)。第三方归属与许可说明见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。
