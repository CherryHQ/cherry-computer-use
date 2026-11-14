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
  options: CallOptions = {},
  taskOptions?: Wire.TaskOptions
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
    if (taskOptions !== undefined) {
      input(taskOptions, validators.validateTaskOptions)
      await connection.request('configureTask', taskOptions, validators.validateTaskOptionsResult, options)
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
    getAppIcon: async (params, options) => {
      input(params, validators.validateAppIconInput)
      return connection.request('getAppIcon', params, validators.validateAppIconResult, options)
    },
    listApps: (options) =>
      connection.request('listApps', {}, validators.validateAppList, options),
    resolveAppIdentity: async (params, options) => {
      input(params, validators.validateAppIdentityInput)
      return connection.request('resolveAppIdentity', params, validators.validateAppIdentity, options)
    },
    resolveApp: async (params, options) => {
      input(params, validators.validateResolveAppInput)
      return connection.request('resolveApp', params, validators.validateAppInfo, options, params.launch === false ? 'none' : 'possible')
    },
    openAppSession: async (params, options) => {
      input(params, validators.validateOpenAppSessionInput)
      return connection.request(
        'openAppSession',
        params,
        validators.validateAppSession,
        options,
        'none',
        (value) => value.app.id === params.appId && value.status === 'active'
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
        validators.validateStopAppSessionResult,
        options,
        'none',
        (value) => value.appSessionId === params.appSessionId
      )
    },
    query: async (params, options) => {
      input(params, validators.validateQueryInput)
      const result = await connection.request('query', params, validators.validateQueryResult, options, 'possible',
        (value) => value.snapshot.appSessionId === params.appSessionId)
      return { ...result, snapshot: decodeSnapshot(result.snapshot) }
    },
    getAppState: async (params, options) => {
      input(params, validators.validateObserveInput)
      return decodeSnapshot(
        await connection.request(
          'getAppState',
          { activation: 'never', ...params },
          validators.validateSnapshot,
          options,
          taskOptions?.lockedUse || params.activation === 'allow' || (params.windowPlacement && params.windowPlacement !== 'keep') ? 'possible' : 'none',
          (value) => value.appSessionId === params.appSessionId
        )
      )
    },
    perform: async (params, options) => {
      input(params, validators.validateAction)
      return connection.request(
        'perform',
        { allowGlobalInput: false, ...params },
        validators.validatePerformResult,
        options,
        'possible',
        (value) => value.appSessionId === params.appSessionId
      )
    },
    act: async (params, options) => {
      input(params, validators.validateAction)
      const result = await connection.request(
        'act',
        { allowGlobalInput: false, ...params },
        validators.validateActionResult,
        options,
        'possible',
        (value) =>
          value.observation.status === 'unavailable' ||
          value.observation.snapshot.appSessionId === params.appSessionId
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
    if (options.taskOptions !== undefined) input(options.taskOptions, validators.validateTaskOptions)
    const executable = await resolveRuntimePath(options.runtimePath)
    checkOptions(callOptions)
    const sessionId = randomUUID()
    const connection = new RuntimeConnection(
      launchRuntime(executable, sessionId),
      sessionId,
      options.taskOptions?.lockedUse || options.taskOptions?.virtualDisplay ? 15_000 : 2_000
    )
    return connectClient(connection, sessionId, callOptions, options.taskOptions)
  }
}
