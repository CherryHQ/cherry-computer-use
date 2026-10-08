# Cherry Computer Use CLI

The native runtime and MCP launcher for Cherry Studio. This private workspace is currently for local packaging only; it is not published to npm.

```sh
npm install -g /path/to/cherrystudio-computer-use-cli-<version>.tgz
cherry-computer-use doctor
cherry-computer-use mcp
```

Existing `open-computer-use` and `ocu` commands remain available. macOS 14+ is required. Grant permissions to **Cherry Computer Use**; permissions from the upstream app do not transfer.

The package includes macOS universal, Linux arm64/x64 and Windows arm64/x64 runtimes. The TypeScript SDK is published separately as `@cherrystudio/computer-use`; use an explicit `runtimePath` with these artifacts.

See [release guide](../../docs/releases/RELEASE_GUIDE.md) for local packaging and signing.
