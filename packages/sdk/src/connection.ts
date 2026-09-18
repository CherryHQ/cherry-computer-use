import type { ChildProcessWithoutNullStreams } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { JSONRPCClient, JSONRPCErrorException } from 'json-rpc-2.0'
import { StreamMessageReader, StreamMessageWriter } from 'vscode-jsonrpc/node'
import { ComputerUseError } from './errors.js'
import type { Effect } from './generated/protocol.js'
import {
  validateErrorData,
  validateRpcResponse,
  validateShutdownResult
} from './generated/validators.cjs'
import type { CallOptions, ClosedEvent } from './types.js'

const CLEANUP_TIMEOUT_MS = 2_000

export function checkOptions(options: CallOptions): number {
  const timeout = options.timeoutMs ?? 30_000
  if (!Number.isInteger(timeout) || timeout < 1 || timeout > 2_147_483_647) {
    throw new ComputerUseError(
      'INVALID_ARGUMENT',
      'timeoutMs must be an integer between 1 and 2147483647'
    )
  }
  if (options.signal?.aborted)
    throw new ComputerUseError('CANCELLED', 'Request cancelled before dispatch')
  return timeout
}

async function bounded<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(
          () =>
            reject(
              new ComputerUseError(
                'CLEANUP_FAILED',
                'Runtime cleanup deadline exceeded'
              )
            ),
          timeoutMs
        )
      })
    ])
  } finally {
    clearTimeout(timer)
  }
}

export class RuntimeConnection {
  private readonly rpc: JSONRPCClient
  private readonly reader: StreamMessageReader
  private readonly writer: StreamMessageWriter
  private readonly exited: Promise<number | null>
  private readonly pending = new Set<{
    cancel: () => void
    done: Promise<void>
  }>()
  private readonly listeners = new Set<(event: ClosedEvent) => void>()
  private closePromise: Promise<void> | undefined
  private closedEvent: ClosedEvent | undefined
  private failure: ComputerUseError | undefined
  private closing = false
  private acknowledged = false

  constructor(
    private readonly child: ChildProcessWithoutNullStreams,
    private readonly sessionId: string
  ) {
    child.stderr.resume()
    this.exited = new Promise((resolve) => {
      child.once('exit', (code) => {
        resolve(code)
        if (!this.closing)
          this.fail(
            new ComputerUseError(
              'RUNTIME_EXITED',
              'Runtime exited unexpectedly'
            )
          )
      })
      child.once('error', (cause) => {
        if (child.pid === undefined) resolve(null)
        this.fail(
          new ComputerUseError(
            'RUNTIME_START_FAILED',
            'Runtime process failed',
            { cause }
          )
        )
      })
    })
    this.reader = new StreamMessageReader(child.stdout)
    this.writer = new StreamMessageWriter(child.stdin)
    this.rpc = new JSONRPCClient((message) => {
      // A blocked write must not prevent rejection when the connection is closed.
      void this.writer.write(message).catch((cause) => {
        this.fail(
          new ComputerUseError('PROTOCOL_ERROR', 'Runtime write failed', {
            cause
          })
        )
      })
    })
    this.reader.onError((cause) =>
      this.fail(
        new ComputerUseError('PROTOCOL_ERROR', 'Runtime read failed', { cause })
      )
    )
    this.writer.onError(([cause]) =>
      this.fail(
        new ComputerUseError('PROTOCOL_ERROR', 'Runtime write failed', {
          cause
        })
      )
    )
    this.reader.onClose(() => {
      if (!this.acknowledged)
        this.fail(
          new ComputerUseError(
            'RUNTIME_EXITED',
            'Runtime connection closed without cleanup acknowledgement'
          )
        )
    })
    this.reader.listen((message) => {
      if (!validateRpcResponse(message)) {
        this.fail(
          new ComputerUseError('PROTOCOL_ERROR', 'Invalid JSON-RPC response')
        )
        return
      }
      this.rpc.receive(message)
    })
  }

  async request<T>(
    method: string,
    params: object,
    validate: (value: unknown) => value is T,
    options: CallOptions = {},
    uncertainEffect: Effect = 'none'
  ): Promise<T> {
    if (this.closing)
      throw new ComputerUseError(
        'CLOSED',
        'Computer Use client is closing or closed'
      )
    const timeoutMs = checkOptions(options)
    const id = randomUUID()
    let cancellation: 'CANCELLED' | 'TIMEOUT' | undefined
    let grace: ReturnType<typeof setTimeout> | undefined
    const cancel = (code: 'CANCELLED' | 'TIMEOUT') => {
      if (cancellation) return
      cancellation = code
      this.rpc.notify('$/cancelRequest', { id })
      grace = setTimeout(
        () =>
          this.fail(
            new ComputerUseError(code, 'Runtime did not confirm cancellation', {
              effect: uncertainEffect,
              cleanup: 'unconfirmed'
            })
          ),
        CLEANUP_TIMEOUT_MS
      )
    }
    const abort = () => cancel('CANCELLED')
    const timer = setTimeout(() => cancel('TIMEOUT'), timeoutMs)
    const settled = Promise.withResolvers<void>()
    const pending = { cancel: abort, done: settled.promise }
    this.pending.add(pending)
    options.signal?.addEventListener('abort', abort, { once: true })
    try {
      const value = await this.sendRequest(id, method, params)
      if (!validate(value)) {
        const error = new ComputerUseError(
          'PROTOCOL_ERROR',
          `Invalid ${method} response`,
          { effect: uncertainEffect }
        )
        this.fail(error)
        throw error
      }
      return value
    } catch (cause) {
      if (cause instanceof ComputerUseError) throw cause
      if (
        cause instanceof JSONRPCErrorException &&
        validateErrorData(cause.data)
      ) {
        const error = new ComputerUseError(
          cancellation ?? cause.data.code,
          cause.message,
          {
            effect: cause.data.effect,
            ...(cause.data.code === 'CLEANUP_FAILED'
              ? { cleanup: 'unconfirmed' as const }
              : {}),
            cause
          }
        )
        if (cause.data.code === 'CLEANUP_FAILED') this.fail(error)
        throw error
      }
      const error = new ComputerUseError(
        cancellation ?? this.failure?.code ?? 'PROTOCOL_ERROR',
        this.failure?.message ?? `Invalid ${method} error response`,
        {
          effect: uncertainEffect,
          cleanup: 'unconfirmed',
          cause
        }
      )
      this.fail(error)
      throw error
    } finally {
      clearTimeout(timer)
      clearTimeout(grace)
      options.signal?.removeEventListener('abort', abort)
      this.pending.delete(pending)
      settled.resolve()
    }
  }

  onClosed(listener: (event: ClosedEvent) => void): () => void {
    if (this.closedEvent) listener(this.closedEvent)
    else this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  fail(error: ComputerUseError): void {
    if (this.closedEvent) return
    this.failure ??= error
    this.closing = true
    this.disposeTransport()
    void this.close().catch(() => {})
  }

  close(): Promise<void> {
    if (!this.closePromise) {
      this.closing = true
      this.closePromise = this.finishClose()
    }
    return this.closePromise
  }

  private async sendRequest(
    id: string,
    method: string,
    params: object
  ): Promise<unknown> {
    const response = await this.rpc.requestAdvanced({
      jsonrpc: '2.0',
      id,
      method,
      params
    })
    if (response.error)
      throw new JSONRPCErrorException(
        response.error.message,
        response.error.code,
        response.error.data
      )
    return response.result
  }

  private disposeTransport(): void {
    this.rpc.rejectAllPendingRequests('Runtime connection closed')
    this.reader.dispose()
    this.writer.dispose()
  }

  private async finishClose(): Promise<void> {
    let cleanup: ClosedEvent['cleanup'] = 'unconfirmed'
    let error: ComputerUseError | undefined
    try {
      if (!this.failure) {
        for (const request of this.pending) request.cancel()
      }
      await bounded(
        Promise.all([...this.pending].map((request) => request.done)),
        CLEANUP_TIMEOUT_MS
      )
      if (this.failure) throw this.failure
      const result = await bounded(
        this.sendRequest(randomUUID(), 'shutdown', {
          sessionId: this.sessionId
        }),
        CLEANUP_TIMEOUT_MS
      )
      if (
        !validateShutdownResult(result) ||
        result.sessionId !== this.sessionId
      ) {
        throw new ComputerUseError(
          'PROTOCOL_ERROR',
          'Invalid runtime cleanup acknowledgement'
        )
      }
      this.acknowledged = true
      if ((await bounded(this.exited, CLEANUP_TIMEOUT_MS)) !== 0) {
        throw new ComputerUseError(
          'RUNTIME_EXITED',
          'Runtime exited unsuccessfully after shutdown'
        )
      }
      cleanup = 'complete'
    } catch (cause) {
      error = new ComputerUseError(
        'CLEANUP_FAILED',
        'Runtime cleanup could not be confirmed',
        { cleanup, cause }
      )
      this.disposeTransport()
      this.child.kill('SIGKILL')
      try {
        await bounded(this.exited, CLEANUP_TIMEOUT_MS)
      } catch {
        error = new ComputerUseError(
          'CLEANUP_FAILED',
          'Runtime process exit could not be confirmed',
          { cleanup, cause }
        )
      }
    } finally {
      this.disposeTransport()
      this.child.stdin.destroy()
      this.child.stdout.destroy()
      this.child.stderr.destroy()
      this.closedEvent = error ? { cleanup, error } : { cleanup }
      for (const listener of this.listeners) {
        // Consumer callbacks must not interrupt process cleanup or other subscribers.
        try {
          listener(this.closedEvent)
        } catch {}
      }
      this.listeners.clear()
    }
    if (error) throw error
  }
}
