import assert from 'node:assert/strict'
import { spawn, execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { once } from 'node:events'
import { mkdtemp, readFile, rm, symlink } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import Ajv from 'ajv'
import { ComputerUse } from '../packages/sdk/dist/index.js'

const runtimePath = resolve(process.env.COMPUTER_USE_RUNTIME_PATH ?? (
  process.platform === 'darwin' ? 'dist/Open Computer Use (Dev).app'
    : process.platform === 'win32' ? 'dist/native/open-computer-use.exe' : 'dist/native/open-computer-use'
))
const executable = process.platform === 'darwin' ? join(runtimePath, 'Contents/MacOS/OpenComputerUse') : runtimePath
const schema = JSON.parse(await readFile(new URL('./schema.json', import.meta.url)))
const ajv = new Ajv({ strict: false })
ajv.addSchema(schema)
const validateResponse = ajv.compile({ $ref: `${schema.$id}#/definitions/RpcResponse` })

function connection(t) {
  const sessionId = randomUUID()
  const child = spawn(executable, ['serve', '--stdio', '--session-id', sessionId], { stdio: 'pipe', windowsHide: true })
  let stderr = ''
  child.stderr.on('data', data => { stderr += data })
  child.stdin.on('error', () => {})
  const exit = once(child, 'exit')
  const messages = []
  const waiters = []
  let buffer = Buffer.alloc(0)
  child.stdout.on('data', chunk => {
    buffer = Buffer.concat([buffer, chunk])
    while (true) {
      const end = buffer.indexOf('\r\n\r\n')
      if (end < 0) break
      const match = /^Content-Length: (\d+)$/im.exec(buffer.subarray(0, end).toString('ascii'))
      assert.ok(match, 'stdout must contain only framed protocol messages')
      const length = Number(match[1])
      if (buffer.length < end + 4 + length) break
      const message = JSON.parse(buffer.subarray(end + 4, end + 4 + length))
      buffer = buffer.subarray(end + 4 + length)
      assert.ok(validateResponse(message), JSON.stringify(validateResponse.errors))
      const waiter = waiters.shift()
      if (waiter) waiter(message)
      else messages.push(message)
    }
  })
  t.after(async () => {
    child.stdin.end()
    if (child.exitCode === null && child.signalCode === null) {
      const exited = await Promise.race([exit.then(() => true), delay(3000).then(() => false)])
      if (!exited) { child.kill('SIGKILL'); await exit }
    }
  })
  return {
    child, sessionId, exit, stderr: () => stderr,
    async receive() {
      if (messages.length) return messages.shift()
      let timer
      try {
        return await Promise.race([
          new Promise(resolve => waiters.push(resolve)),
          new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`Response timeout: ${stderr}`)), 10000) })
        ])
      } finally { clearTimeout(timer) }
    },
    send(id, method, params, fragment = false) {
      const body = Buffer.from(JSON.stringify({ jsonrpc: '2.0', ...(id === undefined ? {} : { id }), method, params }))
      const frame = Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`), body])
      if (fragment) {
        for (let offset = 0; offset < frame.length; offset += 7) child.stdin.write(frame.subarray(offset, offset + 7))
      } else child.stdin.write(frame)
    },
    async initialize() {
      this.send('初始化🙂', 'initialize', { sessionId, protocolVersion: 2 }, true)
      const response = await this.receive()
      assert.equal(response.id, '初始化🙂')
      assert.equal(response.result.sessionId, sessionId)
      assert.equal(response.result.ownership, 'private')
    }
  }
}

function agentPIDs(sessionId) {
  return execFileSync('ps', ['-axo', 'pid=,command='], { encoding: 'utf8' }).split('\n')
    .filter(line => line.includes('__computer-use-sdk-agent') && line.includes(sessionId))
    .map(line => Number(line.trim().split(/\s+/)[0]))
}

async function expectAgentExit(sessionId) {
  for (let attempt = 0; attempt < 40; attempt++) {
    if (agentPIDs(sessionId).length === 0) return
    await delay(100)
  }
  assert.deepEqual(agentPIDs(sessionId), [], 'owned app agent leaked')
}

test('public SDK starts two isolated native sessions and closes them independently', { timeout: 30000 }, async t => {
  const first = await ComputerUse.start({ runtimePath })
  t.after(() => first.close().catch(() => {}))
  const second = await ComputerUse.start({ runtimePath })
  t.after(() => second.close().catch(() => {}))
  const capabilities = await first.getCapabilities()
  assert.equal(capabilities.platform, process.platform)
  assert.deepEqual(await first.listAppSessions(), [])
  await assert.rejects(first.openAppSession({ appId: 'foreign' }), error => error.code === 'TARGET_UNAVAILABLE')
  await assert.rejects(first.stopAppSession({ appSessionId: 'foreign' }), error => error.code === 'APP_SESSION_NOT_FOUND')
  assert.deepEqual(await first.listAppSessions(), [])
  assert.equal(capabilities.capabilities.length, 9)
  assert.ok(capabilities.capabilities.filter(item => !['accessibility', 'screenshot', 'click'].includes(item.name)).every(item => item.availability.status === 'unsupported'))
  await assert.rejects(first.act({ type: 'click', appSessionId: 'missing', snapshotId: 'not-a-snapshot', x: 0, y: 0 }), error =>
    error.code === 'UNSUPPORTED_CAPABILITY' && error.effect === 'none')
  await first.close()
  const permissions = await second.getPermissionStatus()
  assert.ok(Array.isArray(permissions.permissions))
  await second.close()
})

test('macOS SDK resolves a local bundle symlink before starting the app agent', { skip: process.platform !== 'darwin', timeout: 20000 }, async t => {
  const directory = await mkdtemp(join(tmpdir(), 'computer-use-link-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const linkedPath = join(directory, 'helper-link')
  await symlink(runtimePath, linkedPath)
  const client = await ComputerUse.start({ runtimePath: linkedPath })
  t.after(() => client.close().catch(() => {}))
  try {
    const status = await client.getPermissionStatus()
    assert.deepEqual(status.permissions.map(item => item.id).sort(), ['accessibility', 'screenRecording'])
  } finally { await client.close() }
})

test('native protocol checks ownership, parameters and framing before cleanup acknowledgement', { timeout: 20000 }, async t => {
  const runtime = connection(t)
  runtime.send('early', 'getCapabilities', {})
  assert.equal((await runtime.receive()).error.data.code, 'PROTOCOL_ERROR')
  runtime.send('wrong', 'initialize', { sessionId: 'someone-else', protocolVersion: 2 })
  assert.equal((await runtime.receive()).error.data.code, 'PROTOCOL_MISMATCH')
  runtime.send('old-version', 'initialize', { sessionId: runtime.sessionId, protocolVersion: 1 })
  assert.equal((await runtime.receive()).error.data.code, 'PROTOCOL_MISMATCH')
  await runtime.initialize()
  if (process.platform === 'darwin') assert.equal(agentPIDs(runtime.sessionId).length, 1)
  runtime.send('params', 'getPermissionStatus', { unexpected: true })
  assert.equal((await runtime.receive()).error.data.code, 'INVALID_ARGUMENT')
  runtime.send('unscoped', 'getAppState', { appId: 'foreign' })
  assert.equal((await runtime.receive()).error.data.code, 'INVALID_ARGUMENT')
  if (process.platform === 'darwin') {
    runtime.send('bad-permissions', 'requestPermissions', { ids: ['accessibility', 'unknown'] })
    const rejected = await runtime.receive()
    assert.equal(rejected.error.data.code, 'INVALID_ARGUMENT')
    assert.equal(rejected.error.data.effect, 'none')
  }
  runtime.send('wrong-close', 'shutdown', { sessionId: 'someone-else' })
  assert.equal((await runtime.receive()).error.data.code, 'INVALID_ARGUMENT')
  runtime.send('close', 'shutdown', { sessionId: runtime.sessionId })
  assert.deepEqual((await runtime.receive()).result, { sessionId: runtime.sessionId, cleanup: 'complete' })
  if (process.platform === 'darwin') assert.deepEqual(agentPIDs(runtime.sessionId), [], 'acknowledged before agent exit')
  assert.deepEqual(await runtime.exit, [0, null], runtime.stderr())
})

test('owner EOF closes native session and its macOS agent', { timeout: 20000 }, async t => {
  const runtime = connection(t)
  await runtime.initialize()
  runtime.child.stdin.end()
  assert.deepEqual(await runtime.exit, [0, null], runtime.stderr())
  if (process.platform === 'darwin') await expectAgentExit(runtime.sessionId)
})

test('malformed frames fail closed and release the native session', { timeout: 20000 }, async t => {
  for (const frame of ['Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}', 'Content-Length: 2\r\n\r\n']) {
    const runtime = connection(t)
    await runtime.initialize()
    runtime.child.stdin.end(frame)
    const [code] = await runtime.exit
    assert.notEqual(code, 0, 'invalid or truncated frame must not be accepted as a clean EOF')
    if (process.platform === 'darwin') await expectAgentExit(runtime.sessionId)
  }
})

test('macOS app agent exits when its proxy is killed', { skip: process.platform !== 'darwin', timeout: 20000 }, async t => {
  const runtime = connection(t)
  await runtime.initialize()
  assert.equal(agentPIDs(runtime.sessionId).length, 1)
  runtime.child.kill('SIGKILL')
  await runtime.exit
  await expectAgentExit(runtime.sessionId)
})
