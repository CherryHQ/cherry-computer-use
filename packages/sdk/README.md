# @cherrystudio/computer-use

TypeScript client for the Cherry Computer Use native runtime, for Node 24+ and Electron's main process.

**Status: native app control sessions and the first desktop slice are implemented.** All three runtimes expose private sessions, application discovery, structured observation and one semantic left click on an element. Real SDK desktop tests pass on Windows 11 ARM64 and Linux X11; macOS lifecycle tests pass, while desktop validation awaits Accessibility and Screen Recording grants. Coordinate/multiple/right clicks, the other six actions and Wayland capture remain unsupported. macOS explicit permission requests are implemented. SDK/platform packages have not been published, and Cherry development integration now covers permission queries and requests; packaged Electron delivery remains pending.

## Development

From the repository root:

```sh
npm install
npm run sdk:check
npm run sdk:test
npm run sdk:build
```

`protocol/schema.json` generates TypeScript wire types and standalone validators during the build. Generated files are ignored; edit the schema and rebuild. `dist/` contains ESM, CJS and their type declarations. The tarball test installs the built package into an isolated consumer and checks both module formats and TypeScript resolution.

## API

The current runtime supports this lifecycle example. Supply a locally built complete `.app` on macOS or an executable on Windows/Linux:

```ts
import { ComputerUse } from '@cherrystudio/computer-use'

const computer = await ComputerUse.start({ runtimePath })
try {
  const capabilities = await computer.getCapabilities()
  const permissions = await computer.getPermissionStatus()
} finally {
  await computer.close()
}
```

macOS permission queries use non-interactive preflight under the private app agent's identity. Windows/Linux return an empty permission list because this slice has no OS permission flow; this does not imply authorization. `requestPermissions` is unsupported on Windows/Linux. On macOS, an explicit request validates all IDs before interaction and opens the existing native onboarding window with a draggable helper app tile beside System Settings. The request stays pending until a drag is accepted, the requested permissions are observed as granted, or the user selects **Done** or closes the window. An accepted drag ends this interaction even if macOS still needs confirmation; recheck and request any remaining permissions in a new session. It returns observed preflight state, not a promise of approval; dismissing without granting is allowed. Cancellation closes the onboarding window and drag panel before acknowledging cleanup. Windows requires Windows PowerShell and an interactive desktop; Linux uses Go directly with the desktop AT-SPI D-Bus service and X11 for capture, without Python or cgo.

For permission onboarding, keep the client alive while awaiting `requestPermissions`, then close it in `finally`. Query again with a fresh client after onboarding ends, because macOS can cache grants in the old process. The SDK window does not relaunch or terminate the protocol process; the host owns that lifecycle. Keep the signed helper bundle identity stable; the host’s own permissions do not substitute for helper grants.

The following example targets the repository's counter fixture. Choose an authorized target in the host and keep one client for a control task, across multiple tool calls:

```ts
import { ComputerUse } from '@cherrystudio/computer-use'

const computer = await ComputerUse.start({ runtimePath })
try {
  const apps = await computer.listApps()
  const app = apps.find(app => app.name === 'CherrySDKFixture')
  if (!app) throw new Error('Start the counter fixture first')

  const session = await computer.openAppSession({ appId: app.id }, { signal })
  const state = await computer.getAppState({ appSessionId: session.id }, { signal })
  if (state.tree.status === 'available') {
    const element = state.tree.elements.find(element => element.name === 'Count: 0' && element.actions.includes('click'))
    if (element) {
      const result = await computer.act({
        type: 'click', appSessionId: session.id, snapshotId: state.id, elementId: element.id
      }, { signal })
      // Inspect result.observation before choosing another action.
    }
  }
} finally {
  await computer.close()
}
```

- `ComputerUse.start(options?, callOptions?)`: explicitly resolve, launch and initialize a private runtime. Importing the package does not start a process or request permissions.
- `getCapabilities()` / `getPermissionStatus()`: query implemented capabilities and OS permission state. They do not replace the host's user authorization.
- `requestPermissions({ ids })`: explicit macOS OS permission flow for `accessibility` and `screenRecording`; unsupported on Windows/Linux.
- `listApps()` / `openAppSession({ appId })`: discover targets and create per-app control contexts. Opening an active target reuses its context.
- `listAppSessions()`: query native control status (`active`, `stopping`, `stopped`) independently of queued desktop work.
- `stopAppSession({ appSessionId })`: block new/queued work for that context, cancel its active request, and wait for native cleanup. It does not quit the app or close other contexts.
- `getAppState({ appSessionId })`: obtain structured observations for an active app context. Observation defaults to `activation: 'never'`; unsupported constraints must be rejected by native code.
- `act(action)`: the contract includes `click`, `performSecondaryAction`, `scroll`, `drag`, `typeText`, `pressKey`, or `setValue`. Only single left element clicks are connected in this slice. All actions reference an `appSessionId` and its `snapshotId`. Click takes either `elementId` or `x/y`; coordinates refer to returned screenshot pixels. `allowGlobalInput` defaults to `false` and is a native execution constraint, not proof of host authorization.
- `onClosed(listener)`: final cleanup status; returns an unsubscribe function. A late subscriber immediately receives the final event.
- `close()`: idempotent; cancel pending requests, require the owning runtime's cleanup acknowledgement, and wait for its process to exit.

`Snapshot.tree` and `Snapshot.screenshot` independently report `available` or `unavailable`. Available trees include elements and truncation reasons. Available screenshots contain `image.data: Uint8Array`, PNG MIME type and dimensions. Capture must identify one application window; ambiguous mappings return unavailable, with no whole-screen fallback. Element bounds are not exposed in this slice. Model-specific text/image blocks belong in the host adapter.

Protocol v2 requires matching rebuilt SDK/runtime artifacts; restart a running host after updating local links. Old unscoped observation/action calls are rejected. The SDK validates that snapshots and stop acknowledgements belong to the requested app session.

Stopping is irreversible once dispatched, including if the caller cancels the stop request. Repeating a stop is safe; explicitly reopening a stopped target creates a fresh ID. The host must retain the user's stop decision and prevent tools/scripts from automatically reopening the app or recreating the runtime. That Cherry host policy, Tray UI and per-app cursor feedback remain pending.

## Cancellation and failures

Calls accept `signal` and `timeoutMs` (default 30 seconds, except `requestPermissions`, which allows 5 minutes for human interaction). An explicit `timeoutMs` overrides either default. Cancellation sends `$/cancelRequest` and waits up to two additional seconds for the original request's terminal response. A confirmed completed action can win a cancellation race and is returned as completed. If cancellation cannot be confirmed, the client becomes unusable and starts closing.

`ComputerUseError` includes `code`, `effect` (`none`, `possible`, `applied`) and, when relevant, `cleanup`. An action that completed but whose subsequent observation failed returns `status: 'completed'` with `observation.status: 'unavailable'`. Neither that result nor an uncertain error causes automatic retry. Unconfirmed app cleanup returns `CLEANUP_FAILED` with `cleanup: unconfirmed`, disables further calls and closes the connection. A native action with an uncertain outcome disables further desktop operations in that session; close it before starting a new task.

Normal shutdown confirms both the session ID and `cleanup: 'complete'`, then requires exit code 0. Failed cleanup force-terminates the directly owned process, rejects `close()` with `CLEANUP_FAILED`, and reports `cleanup: 'unconfirmed'`. Killing a proxy cannot prove that a native helper released its resources. The native implementation must enforce owner-disconnect cleanup.

After an unexpected disconnection, await `close()` to finish local process cleanup. Closing or closed clients reject new calls with `CLOSED`; there is no reconnect, replay or snapshot restoration.

## Runtime discovery and distribution

An explicit `runtimePath` is a complete `.app` bundle on macOS, or an executable on Windows/Linux. Paths may contain spaces. The SDK uses no shell and launches `serve --stdio --session-id <uuid>`. The macOS bundle's executable must be `Contents/MacOS/OpenComputerUse`.

Without an explicit path, the resolver expects `@cherrystudio/computer-use-{darwin|win32|linux}-{arm64|x64}` at the exact SDK package version. Each package must expose `package.json` and contain:

- macOS: `runtime/Open Computer Use.app/Contents/MacOS/OpenComputerUse` within its complete app bundle.
- Windows: `runtime/open-computer-use.exe`.
- Linux: `runtime/open-computer-use`.

These platform packages and `optionalDependencies` will be added in the distribution stage. Missing packages raise `RUNTIME_NOT_FOUND`; there is no PATH lookup or network installation at runtime. Electron hosts will need to provide the executable resource path outside ASAR.

Code Mode is optional and deferred. Ordinary tools and a future script facade can consume the same client; the SDK contains no interpreter, model SDK or Agent session storage.
