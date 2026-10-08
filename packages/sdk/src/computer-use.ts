import { randomUUID } from 'node:crypto'
import { checkOptions, RuntimeConnection } from './connection.js'
import { ComputerUseError } from './errors.js'
import type * as Wire from './generated/protocol.js'
import * as validators from './generated/validators.cjs'
import { launchRuntime, resolveRuntimePath } from './runtime.js'
import type {
  CallOptions,
  ComputerUseClient,
  Snapshot,
  StartOptions
} from './types.js'

function input<T>(
  value: unknown,
  validate: (value: unknown) => value is T
): asserts value is T {
  if (!validate(value))
    throw new ComputerUseError(
      'INVALID_ARGUMENT',
      'Invalid Computer Use request parameters'
    )
}

function decodeSnapshot(snapshot: Wire.Snapshot): Snapshot {
  if (snapshot.screenshot.status === 'unavailable')
    return { ...snapshot, screenshot: snapshot.screenshot }
  const { dataBase64, ...image } = snapshot.screenshot.image
  return {
    ...snapshot,
    screenshot: {
      status: 'available',
      image: {
        ...image,
        data: new Uint8Array(Buffer.from(dataBase64, 'base64'))
      }
    }
  }
}

export async function connectClient(
  connection: RuntimeConnection,
  sessionId: string,
  options: CallOptions = {}
): Promise<ComputerUseClient> {
  try {
    const result = await connection.request(
      'initialize',
      { protocolVersion: 2, sessionId },
      validators.validateInitializeResult,
      options
    )
    if (
      result.protocolVersion !== 2 ||
      result.sessionId !== sessionId ||
      result.ownership !== 'private'
    ) {
      throw new ComputerUseError(
        'PROTOCOL_MISMATCH',
        'Runtime does not support this private SDK session'
      )
    }
  } catch (cause) {
    try {
      await connection.close()
    } catch {}
    throw cause
  }
  return {
    getCapabilities: (options) =>
      connection.request(
        'getCapabilities',
        {},
        validators.validateCapabilities,
        options
      ),
    getPermissionStatus: (options) =>
      connection.request(
        'getPermissionStatus',
        {},
        validators.validatePermissionStatus,
        options
      ),
    requestPermissions: async (params, options) => {
      input(params, validators.validatePermissionRequest)
      return connection.request(
        'requestPermissions',
        params,
        validators.validatePermissionStatus,
        { ...options, timeoutMs: options?.timeoutMs ?? 300_000 },
        'possible'
      )
    },
    listApps: (options) =>
      connection.request('listApps', {}, validators.validateAppList, options),
    openAppSession: async (params, options) => {
      input(params, validators.validateOpenAppSessionInput)
      return connection.request(
        'openAppSession',
        params,
        (value): value is Wire.AppSession =>
          validators.validateAppSession(value) &&
          value.app.id === params.appId &&
          value.status === 'active',
        options
      )
    },
    listAppSessions: (options) =>
      connection.request(
        'listAppSessions',
        {},
        validators.validateAppSessionList,
        options
      ),
    stopAppSession: async (params, options) => {
      input(params, validators.validateAppSessionInput)
      return connection.request(
        'stopAppSession',
        params,
        (value): value is Wire.StopAppSessionResult =>
          validators.validateStopAppSessionResult(value) &&
          value.appSessionId === params.appSessionId,
        options
      )
    },
    getAppState: async (params, options) => {
      input(params, validators.validateObserveInput)
      return decodeSnapshot(
        await connection.request(
          'getAppState',
          { activation: 'never', ...params },
          (value): value is Wire.Snapshot =>
            validators.validateSnapshot(value) &&
            value.appSessionId === params.appSessionId,
          options,
          params.activation === 'allow' ? 'possible' : 'none'
        )
      )
    },
    act: async (params, options) => {
      input(params, validators.validateAction)
      const result = await connection.request(
        'act',
        { allowGlobalInput: false, ...params },
        (value): value is Wire.ActionResult =>
          validators.validateActionResult(value) &&
          (value.observation.status === 'unavailable' ||
            value.observation.snapshot.appSessionId === params.appSessionId),
        options,
        'possible'
      )
      return {
        status: result.status,
        observation:
          result.observation.status === 'available'
            ? {
                status: 'available',
                snapshot: decodeSnapshot(result.observation.snapshot)
              }
            : result.observation
      }
    },
    onClosed: (listener) => connection.onClosed(listener),
    close: () => connection.close()
  }
}

export const ComputerUse = {
  async start(
    options: StartOptions = {},
    callOptions: CallOptions = {}
  ): Promise<ComputerUseClient> {
    checkOptions(callOptions)
    const executable = await resolveRuntimePath(options.runtimePath)
    checkOptions(callOptions)
    const sessionId = randomUUID()
    const connection = new RuntimeConnection(
      launchRuntime(executable, sessionId),
      sessionId
    )
    return connectClient(connection, sessionId, callOptions)
  }
}
