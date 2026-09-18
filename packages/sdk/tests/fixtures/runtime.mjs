import { setImmediate } from 'node:timers/promises'
import { closeSync } from 'node:fs'
import { Transform } from 'node:stream'
import { createMessageConnection, ResponseError } from 'vscode-jsonrpc/node'

const sessionId = process.argv.includes('--session-id')
  ? process.argv[process.argv.indexOf('--session-id') + 1]
  : process.argv[2]
const mode = process.argv[3] ?? 'normal'
const writer = new Transform({
  transform(chunk, _, callback) {
    void (async () => {
      for (let offset = 0; offset < chunk.length; offset += 17) {
        this.push(chunk.subarray(offset, offset + 17))
        await setImmediate()
      }
    })().then(() => callback(), callback)
  }
})
writer.pipe(process.stdout)
const rpc = createMessageConnection(process.stdin, writer)
let clicks = 0
let generation = 0
const id = () => `${sessionId}:${generation}`
const screenshot = {
  mimeType: 'image/png',
  width: 1,
  height: 1,
  dataBase64:
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6ZAAAAABJRU5ErkJggg=='
}
function snapshot() {
  return {
    id: id(),
    appSessionId: mode === 'foreign-snapshot' ? 'foreign' : 'control',
    app: { id: 'app', name: '测试 🙂' },
    window: { id: 'window', title: 'Fixture' },
    tree: {
      status: 'available',
      truncated: [],
      elements: [
        {
          id: 'button',
          role: 'button',
          name: '按钮',
          value: String(clicks),
          actions: ['click'],
          secondaryActions: []
        }
      ]
    },
    screenshot: { status: 'available', image: screenshot }
  }
}
function error(code, effect = 'none') {
  return new ResponseError(-32000, code, { code, effect })
}
rpc.onRequest('initialize', (input) => {
  if (mode === 'bootstrap-exit') process.exit(8)
  if (mode === 'bootstrap-hang') return new Promise(() => {})
  return {
    protocolVersion: mode === 'mismatch' ? 99 : input.protocolVersion,
    sessionId: mode === 'wrong-owner' ? 'another-owner' : sessionId,
    runtimeVersion: 'fixture',
    ownership: 'private'
  }
})
rpc.onRequest('getCapabilities', () => ({
  platform: process.platform,
  capabilities: [{ name: 'click', availability: { status: 'available' } }]
}))
rpc.onRequest('getPermissionStatus', () => ({ permissions: [] }))
rpc.onRequest('requestPermissions', (input, token) => {
  const status = {
    permissions: input.ids.map((id) => ({
      id,
      label: id,
      status: 'unknown',
      interaction: 'systemSettings'
    }))
  }
  if (mode !== 'permission-wait') return status
  process.send?.('permission-opened')
  return new Promise((resolve) => {
    token.onCancellationRequested(() => process.send?.('permission-cancelled'))
    process.once('message', () =>
      resolve(token.isCancellationRequested ? error('CANCELLED', 'possible') : status)
    )
  })
})
rpc.onRequest('listApps', async () => {
  await setImmediate()
  return [{ id: 'app', name: `clicks:${clicks}` }]
})
let controlStatus
rpc.onRequest('openAppSession', (input) => {
  controlStatus = 'active'
  return { id: 'control', app: { id: input.appId, name: '测试 🙂' }, status: 'active' }
})
rpc.onRequest('listAppSessions', () => [{ id: 'control', app: { id: 'app', name: '测试 🙂' }, status: controlStatus }])
rpc.onRequest('stopAppSession', () => {
  controlStatus = 'stopped'
  if (mode === 'stop-failure') return error('CLEANUP_FAILED', 'possible')
  return { appSessionId: mode === 'wrong-app-stop' ? 'foreign' : 'control', status: 'stopped', cleanup: 'complete' }
})
rpc.onRequest('getAppState', (input) => {
  if (controlStatus !== 'active') return error('APP_SESSION_STOPPED')
  if (input.activation !== 'never') return error('INVALID_ARGUMENT')
  generation++
  if (mode === 'malformed') return { id: 'missing-fields' }
  if (mode === 'exit') process.exit(3)
  if (mode === 'eof') {
    process.stdout.end()
    return new Promise(() => {})
  }
  if (mode === 'null-envelope' || mode === 'ambiguous-envelope') {
    const body = JSON.stringify(
      mode === 'null-envelope'
        ? null
        : {
            jsonrpc: '2.0',
            id: 'invalid',
            result: {},
            error: { code: 0, message: 'bad' }
          }
    )
    writer.write(
      Buffer.from(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`)
    )
    return new Promise(() => {})
  }
  if (mode === 'write-failure' || mode === 'backpressure') {
    process.once('message', () => {
      if (mode === 'write-failure') {
        process.stdin.destroy()
        closeSync(0)
      } else {
        process.stdin.pause()
      }
      setInterval(() => {}, 1_000)
      process.send?.('input-closed')
    })
  }
  return snapshot()
})
rpc.onRequest('act', async (action, token) => {
  if (controlStatus !== 'active') return error('APP_SESSION_STOPPED')
  if (action.snapshotId !== id()) return error('STALE_SNAPSHOT')
  if (action.allowGlobalInput !== false) return error('INVALID_ARGUMENT')
  process.send?.('action-started')
  if (
    mode === 'cancel' ||
    mode === 'ignore-cancel' ||
    mode === 'late-completion'
  ) {
    return new Promise((resolve) =>
      token.onCancellationRequested(() => {
        process.send?.('cancel-received')
        if (mode === 'ignore-cancel') return
        if (mode === 'late-completion') {
          clicks++
          generation++
          resolve({
            status: 'completed',
            observation: { status: 'available', snapshot: snapshot() }
          })
        } else {
          process.once('message', () => resolve(error('CANCELLED')))
        }
      })
    )
  }
  clicks++
  generation++
  if (mode === 'error-after-effect') return error('CAPTURE_FAILED', 'applied')
  if (mode === 'capture-failure')
    return {
      status: 'completed',
      observation: {
        status: 'unavailable',
        reason: { code: 'CAPTURE_FAILED', message: 'Screenshot failed' }
      }
    }
  return {
    status: 'completed',
    observation: { status: 'available', snapshot: snapshot() }
  }
})
rpc.onRequest('shutdown', () => {
  if (mode === 'shutdown-hang') return new Promise(() => {})
  setTimeout(() => {
    rpc.dispose()
    writer.end(() => process.exit(0))
  }, 50)
  return {
    sessionId: mode === 'wrong-shutdown-owner' ? 'another-owner' : sessionId,
    cleanup: 'complete'
  }
})
process.stdin.on('end', () => process.exit(0))
rpc.listen()
