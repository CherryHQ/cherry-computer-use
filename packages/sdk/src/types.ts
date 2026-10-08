import type * as Wire from './generated/protocol.js'
import type { ComputerUseError } from './errors.js'

export interface StartOptions {
  /** Complete .app bundle on macOS; executable on Windows/Linux. */
  runtimePath?: string
}

export interface CallOptions {
  signal?: AbortSignal
  /** Request deadline in milliseconds, excluding bounded cancellation cleanup. Defaults to 30 seconds, or 5 minutes for requestPermissions. */
  timeoutMs?: number
}

export type Screenshot = Omit<Wire.Screenshot, 'dataBase64'> & {
  data: Uint8Array
}
export type Snapshot = Omit<Wire.Snapshot, 'screenshot'> & {
  screenshot:
    | { status: 'available'; image: Screenshot }
    | { status: 'unavailable'; reason: Wire.Failure }
}
export interface ActionResult {
  status: 'completed'
  observation:
    | { status: 'available'; snapshot: Snapshot }
    | { status: 'unavailable'; reason: Wire.Failure }
}

export interface ClosedEvent {
  cleanup: 'complete' | 'unconfirmed'
  error?: ComputerUseError
}

export interface ComputerUseClient {
  getCapabilities(options?: CallOptions): Promise<Wire.Capabilities>
  getPermissionStatus(options?: CallOptions): Promise<Wire.PermissionStatus>
  requestPermissions(
    input: Wire.PermissionRequest,
    options?: CallOptions
  ): Promise<Wire.PermissionStatus>
  listApps(options?: CallOptions): Promise<Wire.AppInfo[]>
  openAppSession(
    input: Wire.OpenAppSessionInput,
    options?: CallOptions
  ): Promise<Wire.AppSession>
  listAppSessions(options?: CallOptions): Promise<Wire.AppSession[]>
  /** Stops automation and waits for native cleanup; does not quit the target app. */
  stopAppSession(
    input: Wire.AppSessionInput,
    options?: CallOptions
  ): Promise<Wire.StopAppSessionResult>
  getAppState(
    input: Wire.ObserveInput,
    options?: CallOptions
  ): Promise<Snapshot>
  act(input: Wire.Action, options?: CallOptions): Promise<ActionResult>
  /** Subscribing after closure immediately receives the final event. */
  onClosed(listener: (event: ClosedEvent) => void): () => void
  close(): Promise<void>
}
