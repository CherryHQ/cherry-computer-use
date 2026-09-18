# SDK protocol v2

Status: client contract, Swift/Go lifecycle servers and the first structured desktop slice are implemented. This protocol is separate from the existing MCP entry point; desktop support is limited to discovery, observation and one semantic left element click.

[schema.json](schema.json) is the source of wire types and validators. Run `npm run sdk:build` from the repository root to regenerate. The [SDK README](../packages/sdk/README.md) documents the consumer API and current implementation limits.

## Transport and ownership

Launch `serve --stdio --session-id <uuid>`. Use JSON-RPC 2.0 over stdin/stdout, with ASCII `Content-Length: <UTF-8 byte count>\r\n\r\n` headers followed by the JSON body. Stdout is protocol-only; stderr is drained separately. Requests use string IDs. No MCP `tools/call` wrapper, batch requests, server-to-client requests or extra notifications are needed in v2.

The client sends `initialize` with protocol version 2 and its session ID. The response must echo the session ID and version, report the runtime version, and acknowledge `ownership: 'private'`. Runtime version is informational for explicitly supplied paths; protocol compatibility is mandatory. Platform npm packages additionally match the SDK package version.

The native server must own a private control connection and its helper resources. On macOS, the SDK-launched executable is a proxy: it must start an isolated app agent under the app bundle's permission identity, forward requests asynchronously, and clean up on owner EOF. A successful proxy handshake alone does not prove these behaviors.

## Methods

| Method | Params definition | Result definition |
| --- | --- | --- |
| `initialize` | `InitializeInput` | `InitializeResult` |
| `getCapabilities` | empty object | `Capabilities` |
| `getPermissionStatus` | empty object | `PermissionStatus` |
| `requestPermissions` | `PermissionRequest` | `PermissionStatus` |
| `listApps` | empty object | `AppList` |
| `openAppSession` | `OpenAppSessionInput` | `AppSession` |
| `listAppSessions` | empty object | `AppSessionList` |
| `stopAppSession` | `AppSessionInput` | `StopAppSessionResult` |
| `getAppState` | `ObserveInput` | `Snapshot` |
| `act` | `Action` | `ActionResult` |
| `shutdown` | object containing the session ID | `ShutdownResult` |

The current servers implement lifecycle methods, `listApps`, `getAppState` and a single semantic left element `click`. Accessibility, click and screenshot availability reflect platform dependencies/permissions; the remaining six capabilities remain unsupported. macOS implements explicit `requestPermissions`; Windows/Linux do not. Coordinate, right/middle and multiple clicks are rejected before input. macOS queries accessibility and screen-capture preflight without prompting, reporting `unknown` when not granted. Windows/Linux return an empty permission list because this slice has no OS permission flow, not because host authorization is implied.

On macOS, `requestPermissions` reuses the native drag-to-add onboarding window for the requested IDs. Its response remains pending until an accepted drag, completion or user dismissal and reports actual preflight state. Drag acceptance ends the UI interaction but never implies an OS grant; the host rechecks the full permission list in a new session. Cancellation and owner EOF close all onboarding windows and monitors before terminal acknowledgement. The SDK client defaults this interactive call to a five-minute deadline; other calls retain thirty seconds. Hosts must await the request before closing its session, then use a fresh session to observe grants that require process restart.

Native framing limits headers to 8 KiB and bodies to 64 MiB; the ordinary execution queue is bounded at 64, with room for up to 64 additional stop requests and 128 pending responses. Request cleanup has a one-second deadline. Shutdown waits for output with a bounded deadline; failure never produces a successful cleanup acknowledgement. A semantic action already dispatched to the OS must settle or fail as uncertain; cancellation does not roll it back. Unresponsive platform calls can cause an unconfirmed close rather than a false cleanup acknowledgement. This slice does not synthesize pressed keys/buttons.

Validate parameters on the native side too. The client validates requests and domain results, but TypeScript types are not an execution boundary. The native service produces structured elements and capture states, with truncation flags; it must not reconstruct these fields from MCP display text. Screenshots are base64 PNG on the wire and decoded to bytes by the SDK.

Native code owns app/window/element IDs and snapshot validity. IDs must not refer to another client or a restarted process. Re-observation or an action that may change the target invalidates the previous snapshot for that target. Native code revalidates process/window/element identity and action support before execution. Coordinate conversion is reserved for a later slice; no coordinate input is currently accepted.

`activation: 'never'` and `allowGlobalInput: false` are defaults sent by the SDK. Native code must honor them or report a structured failure. Host authorization is additional; these input flags do not establish permission to control a target.

## Application control sessions

Version 2 requires an explicit `appSessionId` on every observation and action and
returns it in each snapshot. Version 1 helpers fail initialization rather than
accepting unscoped requests. Rebuild both the SDK and native helper, then restart
the host when updating a local link.

`openAppSession({ appId })` binds a discovered application instance. Reopening an
active target returns its existing context without invalidating snapshots. A
stopping context cannot be reopened until it settles. After a confirmed stop,
explicit opening creates a fresh ID; old IDs and snapshots cannot be reused.
The host must enforce user authorization before opening a new context or runtime.

`listAppSessions()` returns `active`, `stopping` and `stopped` contexts on the
control channel, independently of the desktop queue. `stopAppSession` marks the
context stopping and cancels its active request as soon as the reader receives
it. Queued and new requests for that context cannot start. Cleanup waits for the
active backend call to settle, then drops its snapshots and native references;
only then does it acknowledge the matching ID and `cleanup: complete`.
A dispatched stop is irreversible even if its request is cancelled. Repeating a
stop is safe, and other app contexts remain usable. No target application is quit.

A one-second cleanup deadline produces `CLEANUP_FAILED` and leaves the context
stopping if native work has not settled. The runtime disables desktop operations;
the SDK reports `cleanup: unconfirmed` and closes the connection. Completion may
win cancellation, and an already dispatched semantic action is never rolled back.
The current slice holds no synthetic keys/buttons or software overlay; cleanup
for those resources must be added with their action implementations.

The native registry isolates contexts within one runtime. Cross-task app ownership,
Tray controls and the user-stop latch belong to the Cherry host and are still
pending. An app disappearing from `listApps()` retires its contexts; execution also
revalidates native process/window/element identity. Continuous window/exit tracking
is part of the later overlay integration.

## Cancellation, errors and shutdown

The input loop must continue reading while an action runs. Serialize desktop operations within the connection; process `$/cancelRequest`, app-stop/state requests and `shutdown` independently of the action queue. Cancellation params contain the original request ID. A cancelled queued operation must not start. Send exactly one terminal response for the original request, after cancellation has reached the platform input/bridge and released its input state.

Domain JSON-RPC errors carry `error.data` matching `ErrorData`: a stable domain code and `effect`. Do not infer domain codes from a message string. `effect: 'none'` means no action effect, `possible` means an effect cannot be ruled out, and `applied` means execution is confirmed. A confirmed action with failed post-action capture returns `ActionResult` with unavailable observation rather than an action error. A completion racing cancellation may return the completed result.

`shutdown` stops accepting work, cancels/settles outstanding requests, releases input/overlay resources, and terminates owned helpers. Only then reply with the matching session ID and `cleanup: 'complete'`, flush stdout, and exit with code 0. Never terminate another host's helper. EOF and startup failures also require native cleanup; the client cannot prove cleanup merely by killing a proxy.

The client waits two seconds for cancellation confirmation and uses bounded shutdown/exit waits. Failure to confirm causes local process termination and an unconfirmed-cleanup error, never a successful close. The client never automatically reconnects or replays an action.

## Implementation libraries

Framing uses [vscode-jsonrpc stream readers/writers](https://github.com/microsoft/vscode-languageserver-node/blob/main/jsonrpc/README.md); request correlation uses [json-rpc-2.0](https://github.com/shogowada/json-rpc-2.0). The `vscode-jsonrpc` 9.0.2 connection wrapper produced an unhandled rejection on process-start write failure during testing, so it is not used for client request management. No global unhandled-rejection handler is installed.

Wire declarations are generated with `json-schema-to-typescript`; [Ajv standalone](https://ajv.js.org/standalone.html) validators are bundled into the SDK. Native framing/decoding use Swift Foundation and the Go standard library. The [shared Go server](../packages/runtime-go/README.md) owns Windows/Linux request lifecycle; Swift uses `SDKTransport` and `SDKSession`.

## Native verification

Build the SDK with `npm run sdk:build`, build the target runtime, then run `node --test protocol/native.test.mjs`. Set `COMPUTER_USE_RUNTIME_PATH` to a complete macOS `.app` or Windows/Linux executable. Defaults are `dist/Open Computer Use (Dev).app` and `dist/native/open-computer-use[.exe]` respectively.

The [native contract tests](native.test.mjs) consume the public SDK and validate raw replies against the same schema. They cover isolated sessions, ownership, parameter rejection, fragmented UTF-8 frames, EOF, malformed input and macOS agent exit after proxy death. Swift/Go unit tests exercise blocked requests, queued cancellation and cleanup failures. Passing lifecycle tests alone does not establish desktop actions, platform npm packages or Electron packaging.

[desktop.test.mjs](desktop.test.mjs) drives a real counter fixture through two SDK sessions: structured hierarchy, PNG bytes, `Count: 0 → 1 → 2`, foreign IDs, used snapshots, external UI changes, truncation and independent close. Set `COMPUTER_USE_FIXTURE_APP` to the exact fixture name and `COMPUTER_USE_REQUIRE_SCREENSHOT=1` to require capture; otherwise the desktop test is skipped. See [fixture instructions](fixtures/README.md).

The previous desktop slice passed on Windows 11 ARM64 (Parallels) and Linux ARM64 X11 (Python-free Docker). The v2 app-session slice has been rerun on Linux X11, including confirmed app stop and continued control in a second runtime. Windows ARM64 builds with v2; its new desktop run is pending because the local Parallels command channel did not respond. Windows also exercises worker/descendant cleanup under cancellation and owner death. macOS currently has lifecycle/contract evidence only; its GUI test needs explicit OS grants. Remote CI, other Windows versions, Wayland, platform tarballs and Electron remain separate verification work.
