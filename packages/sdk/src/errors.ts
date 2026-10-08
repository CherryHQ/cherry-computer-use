import type { Effect, ErrorCode } from './generated/protocol.js'

export class ComputerUseError extends Error {
  readonly code: ErrorCode
  readonly effect: Effect
  readonly cleanup: 'complete' | 'unconfirmed' | undefined

  constructor(
    code: ErrorCode,
    message: string,
    options: {
      effect?: Effect
      cleanup?: 'complete' | 'unconfirmed'
      cause?: unknown
    } = {}
  ) {
    super(message, { cause: options.cause })
    this.name = 'ComputerUseError'
    this.code = code
    this.effect = options.effect ?? 'none'
    this.cleanup = options.cleanup
  }
}
