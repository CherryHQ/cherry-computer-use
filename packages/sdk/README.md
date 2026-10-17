# @cherrystudio/computer-use

TypeScript client for the Cherry Computer Use native runtime, for Node 24+ and Electron's main process.

**Status: private native app control sessions are implemented on all three platforms.**
macOS and Windows adapt their existing engines for all seven actions. Windows uses
UI Automation and window-directed messages, including coordinate clicks; it never
falls back to global input. Linux supports discovery, observation and semantic
element actions: left click, secondary actions, `setValue`, and `typeText` into the
one focused editable field. On X11, pointer clicks, drag, key presses, page scrolling
and typing without a focused field use global input and require
`allowGlobalInput: true`; keys need the target window to have focus. On Wayland, key
presses, page scrolling and typing without a focused field need
`allowGlobalInput: true`. wlroots compositors such as Sway and Hyprland use a
virtual keyboard with no dialog, can type any character, and require the
compositor to report the target as the focused window. On Sway and Hyprland,
screenshots and pointer input also work for native Wayland windows: the pointer moves
first and a button is pressed only when the compositor then reports the target as
the focused window, which needs focus to follow the mouse (the default). On GNOME,
screenshots, pointer and keyboard input go through the Cherry Computer Use Shell
extension: the user runs `open-computer-use install-gnome-extension` and logs in
again, then `requestPermissions({ ids: ['gnomeShell'] })` shows the Shell's dialog.
The extension captures covered windows, clicks only when the target owns the point,
sends keys only to the focus window, and types any Unicode into text-input clients.
On KDE Plasma 6, `open-computer-use install-kde-integration` authorizes the runtime's
helper for KWin window capture and input (no new login; the `kdeIntegration`
permission reports it): clicks are pressed only once the cursor is at the point and
the target is topmost there, keys only while the target is active, and text the
keyboard layout lacks is refused unless it goes into a focused text field.
Without them, GNOME and KDE keys go through the RemoteDesktop portal (`remoteDesktop`
permission); the target window must be active and characters the keyboard layout
lacks are refused, and capture and pointer input are unavailable. Query capabilities and element actions before
acting. Windows message delivery does not guarantee that every application accepts
background input; multi-window selection and GPU window capture remain limited.
macOS supports explicit permission requests. SDK 0.1.1 includes matching native
packages; the Windows engine adapter changes require a subsequent SDK/native release.


## Development

From the repository root:

```sh
npm install
npm run sdk:check
npm run sdk:test
npm run sdk:build
```

CI can reuse a completed build with `npm run test:built --workspace @cherrystudio/computer-use`. Run this through npm so the tarball consumer test receives `npm_execpath`; it does not rebuild `dist/`.

`protocol/schema.json` generates TypeScript wire types and standalone validators during the build. Generated files are ignored; edit the schema and rebuild. `dist/` contains ESM, CJS and their type declarations. The tarball test installs the built package into an isolated consumer and checks both module formats and TypeScript resolution.

## API

Install with `npm install @cherrystudio/computer-use` (Node 24+, optional dependencies enabled). The native-distribution patch installs the matching helper automatically. SDK 0.1.0 still requires an explicit locally built `runtimePath`:

```ts
import { ComputerUse } from '@cherrystudio/computer-use'

const computer = await ComputerUse.start()
try {
  const capabilities = await computer.getCapabilities()
  const permissions = await computer.getPermissionStatus()
} finally {
  await computer.close()
}
```

macOS permission queries use non-interactive preflight under the private app agent's identity. Windows and Linux X11 return an empty permission list because they have no OS permission flow; this does not imply authorization. Linux Wayland reports `remoteDesktop`, and `requestPermissions({ ids: ['remoteDesktop'] })` shows the RemoteDesktop portal dialog, resolving to `granted` or `denied`; cancelling returns early but cannot close the dialog. `requestPermissions` is unsupported on Windows and Linux X11. On macOS, an explicit request validates all IDs before interaction and opens the existing native onboarding window with a draggable helper app tile beside System Settings. The request stays pending until a drag is accepted, the requested permissions are observed as granted, or the user selects **Done** or closes the window. An accepted drag ends this interaction even if macOS still needs confirmation; recheck and request any remaining permissions in a new session. It returns observed preflight state, not a promise of approval; dismissing without granting is allowed. Cancellation closes the onboarding window and drag panel before acknowledging cleanup. Windows requires Windows PowerShell and an interactive desktop; Linux uses Go directly with the desktop AT-SPI D-Bus service and X11 for capture, without Python or cgo.

For permission onboarding, keep the client alive while awaiting `requestPermissions`, then close it in `finally`. Query again with a fresh client after onboarding ends, because macOS can cache grants in the old process. The SDK window does not relaunch or terminate the protocol process; the host owns that lifecycle. Keep the signed helper bundle identity stable; the host’s own permissions do not substitute for helper grants.

The following example targets the repository's counter fixture. Choose an authorized target in the host and keep one client for a control task, across multiple tool calls:

```ts
import { ComputerUse } from '@cherrystudio/computer-use'

const computer = await ComputerUse.start()
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
- `requestPermissions({ ids })`: explicit OS permission flow for macOS `accessibility` and `screenRecording`, and Linux Wayland `remoteDesktop`; unsupported on Windows and Linux X11.
- `listApps()` / `openAppSession({ appId })`: discover targets and create per-app control contexts. Opening an active target reuses its context. App IDs identify the running process and stay valid across runtime restarts until it exits.
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

- macOS: `runtime/Cherry Computer Use.app/Contents/MacOS/OpenComputerUse` within its complete app bundle.
- Windows: `runtime/open-computer-use.exe`.
- Linux: `runtime/open-computer-use`.

The SDK pins all six platform packages to its exact version with `optionalDependencies`; npm selects the matching OS/CPU. Platform packages contain the native helper and no npm CLI or install scripts. macOS release helpers are Developer ID signed and notarized. Missing packages (including installs with `--omit=optional`) raise `RUNTIME_NOT_FOUND`; there is no PATH lookup or network installation at runtime. Electron hosts must package the helper outside ASAR and supply its resource path when necessary. Signed notarization and Electron packaging are separate acceptance checks.

Code Mode is optional and deferred. Ordinary tools and a future script facade can consume the same client; the SDK contains no interpreter, model SDK or Agent session storage.

## Cherry 发布身份

SDK 包名保持 `@cherrystudio/computer-use`。macOS helper 改为 `Cherry Computer Use.app`（`com.cherryai.cherrystudio.computer-use`），开发版为 `Cherry Computer Use (Dev).app`（`.dev`）。嵌入或显式 `runtimePath` 要更新路径，并重新授予新 Bundle ID 系统权限。CLI/native workspace `@cherrystudio/computer-use-cli` 暂不发布，可在本地构建制品；SDK 平台包随 SDK 同版本交付，自动发现后端；0.1.0 仍需显式指定 runtime。版本与签名、公证流程见 [发布指南](../../docs/releases/RELEASE_GUIDE.md)。

## Persistent automation additions (unreleased)

- `resolveApp({ query })` resolves an unambiguous application name or runtime ID. Matching native builds can also launch installed applications: macOS bundles, Windows AppsFolder applications/executable paths, and Linux XDG desktop entries (D-Bus activation or Exec; no GIO tools required). Resolve first, then open an owned app session. Launch waits for a matching accessible window; obtain a fresh observation before input. Windows/Linux desktop policy may activate the launched application.
- `perform(action)` validates and executes the same `Action` input as `act`, returning `{ status: 'completed', appSessionId }` without capturing another observation. This receipt confirms input dispatch, not the user's task outcome.
- Every action consumes element/coordinate refs. Only `typeText`/`pressKey` through `perform` can reuse the last target snapshot ID, with native target validation. An explicit observation replaces that keyboard target; errors require fresh observation. A different app/window cannot inherit the target.
- `act` retains its existing action-plus-observation behavior. New methods require matching rebuilt SDK/native artifacts. There is no automatic fallback or retry after an uncertain action.

The host owns app selection authorization, persistent proxies, output delivery and permission revocation. These additions do not expose MCP or a JavaScript runtime in the SDK.

`resolveApp({ query, launch: false })` only resolves running targets. Hosts use this when reacquiring a persistent handle so a read cannot silently relaunch an application closed by the user. Explicit binding defaults to allowing launch on supported platforms.

`getAppState({ appSessionId, includeScreenshot: false })` collects AX state without calling the native screenshot backend on macOS, Windows or Linux. Omitted/true preserves capture. A skipped screenshot reports `CAPTURE_NOT_REQUESTED`, not a permission failure. Coordinate input requires a screenshot-enabled observation; semantic input and keyboard targeting remain available. This option requires the matching unreleased SDK/native build.

On macOS, SDK `typeText` sends keyboard text to the validated application PID, including keyboard-driven controls without an editable AX field. It does not replace an AX value or activate the application. Each complete grapheme is sent separately; newline sends Return. `setValue` remains the explicit editable-control value operation. The legacy MCP text-field path and other platforms retain their own input restrictions.

### Authorizing application launch

`resolveAppIdentity({ query })` resolves an installed application without launching
or activating it. It returns a stable `id`, display `name`, and canonical `query`.
macOS accepts bundle names/IDs/paths; Windows accepts AppsFolder names/AUMIDs and
executable names/paths; Linux accepts desktop names/IDs/paths. Authorize identity in the
host before calling `resolveApp({ query, expectedIdentity: id, launch: true })`.
The runtime checks `expectedIdentity` before launch; a changed identity fails
without launching. Running `AppInfo` and app sessions include the same optional
`identity`; their opaque `id` is still only a runtime control reference.

`launch: false` retains its running-only semantics, including installed aliases of
an already running application. Older Windows/Linux runtimes reject installed
identity resolution with `UNSUPPORTED_CAPABILITY`; rebuild/package matching native
artifacts to enable launch. Host stop decisions remain host-owned and must be
checked before effects.

Windows packaged identities use AUMIDs; desktop identities use canonical executable
paths. App launches use the Windows Shell/application activation APIs outside the
temporary UIA worker's kill-on-close Job Object. Linux identities use canonical
desktop file paths; XDG user overrides and localized names are respected. A Flatpak
launcher's identity names its installation's `active` deployment, so it survives
updates; Flatpak apps become resolvable by name once the session's `XDG_DATA_DIRS`
and D-Bus services include Flatpak exports, normally after logging in again. Launch
follows the Desktop Entry Specification on every desktop: `DBusActivatable` entries
receive `org.freedesktop.Application.Activate` on the session bus, falling back to
Exec only when no service is installed; other entries run their expanded Exec
(quoting, field codes, `Path`) detached from the runtime. Exec launches opt the
process into accessibility (`QT_LINUX_ACCESSIBILITY_ALWAYS_ON`, `GNOME_ACCESSIBILITY`,
`ACCESSIBILITY_ENABLED`), since Qt, Firefox and Chromium otherwise expose no tree
until assistive technology is enabled session-wide. Chromium and Electron programs,
recognised by Chromium's resource pack beside the program or inside an AppImage
(mounted read-only, without running its application), also receive
`--force-renderer-accessibility`, without which their page content stays hidden. Confined Snap applications
answer AT-SPI only to peers their AppArmor policy allows, such as an unconfined
host. Terminal entries are not launched. Discovery associates processes through the owner of an activatable
application's bus name, the processes this runtime launched (by PID and start
time), launch metadata, an AppImage's image path for processes running from its
mount, Flatpak application IDs or an unambiguous executable match. Unidentifiable or
ambiguous running targets are never selected by a guessed name after launch.

After dispatch, cancellation, missing accessibility or window timeout reports
`effect: possible`; do not replay launch automatically. These SDK additions do not
change the legacy CLI/MCP launch policy or authorize global input.
