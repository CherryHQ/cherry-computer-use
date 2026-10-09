# Native SDK session (Go)

Shared by the Windows and Linux executables. This package implements the
[SDK protocol](../../protocol/README.md), independently of the legacy MCP server.
It supports initialization, capability/permission queries, app control sessions, cancellation and shutdown.
`Desktop` owns opaque app/snapshot/element identities and combines `DesktopDriver`
observation with snapshot-scoped actions. Windows implements `ActionDriver` through
the existing PowerShell engine for all seven actions, including window-relative
coordinate clicks. Linux implements `ActionDriver` with its shared AT-SPI engine for
semantic actions only and refuses global-input actions before dispatch. Drivers keep
native references outside wire data. Permission requests remain unsupported.

Each app control context owns its snapshots and cancellation state. Observation
and action require its `appSessionId`. App stop/state messages bypass the ordinary
queue: stopping cancels active work, prevents queued work from starting and waits
for cleanup. A failed stop disables desktop operations instead of reporting success.
Windows pairs directed button/key messages with releases in `finally` blocks and
checks cancellation during repeated input; it does not use global input or overlays.

The reader continues receiving control messages while one backend request executes.
Cancellation settles the original request only when its backend returns. Shutdown
waits for requests and backend cleanup before acknowledging, with bounded waits.
The caller owns the input/output streams and transfers their lifetime to `Serve`.

Run `go test -race ./...` in this directory. The tests cover framing, ownership,
cancellation, EOF, backpressure, app-stop under a full queue, context isolation, cleanup failures, stale/foreign snapshots and
completed-action receipts surviving failed observation. Unknown action effects
disable subsequent desktop operations. Real platform tests live in
[protocol/desktop.test.mjs](../../protocol/desktop.test.mjs). JSON decoding and framing use Go's standard library; this limited
server does not need a bidirectional JSON-RPC client or MCP implementation.
