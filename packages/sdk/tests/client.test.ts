import assert from 'node:assert/strict'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { once } from 'node:events'
import { chmod, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setImmediate, setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { test, type TestContext } from 'node:test'
import { ComputerUse, connectClient } from '../src/computer-use.js'
import { RuntimeConnection } from '../src/connection.js'
import { ComputerUseError } from '../src/errors.js'
import type { Action, ErrorCode } from '../src/generated/protocol.js'

const fixture = new URL('./fixtures/runtime.mjs', import.meta.url)

function launch(t: TestContext, mode = 'normal') {
  const sessionId = randomUUID()
  const child = spawn(
    process.execPath,
    [fileURLToPath(fixture), sessionId, mode],
    {
      stdio: ['pipe', 'pipe', 'pipe', 'ipc']
    }
  ) as ChildProcessWithoutNullStreams
  const connection = new RuntimeConnection(child, sessionId)
  t.after(async () => {
    await connection.close().catch(() => {})
    assert.notEqual(child.exitCode === null && child.signalCode === null, true)
  })
  return { child, connection, sessionId }
}

async function start(t: TestContext, mode = 'normal') {
  const runtime = launch(t, mode)
  const client = await connectClient(runtime.connection, runtime.sessionId)
  await client.openAppSession({ appId: 'app' })
  return { ...runtime, client }
}

const hasCode = (code: ErrorCode, effect?: string) => (error: unknown) => {
  assert.ok(error instanceof ComputerUseError)
  assert.equal(error.code, code)
  if (effect !== undefined) assert.equal(error.effect, effect)
  return true
}

test('app sessions expose native stop state and stopped calls cannot execute', async (t) => {
  const { client } = await start(t)
  const session = await client.openAppSession({ appId: 'app' })
  const snapshot = await client.getAppState({ appSessionId: session.id })
  assert.equal(snapshot.appSessionId, session.id)
  assert.equal((await client.listAppSessions())[0]?.status, 'active')
  assert.deepEqual(await client.stopAppSession({ appSessionId: session.id }), {
    appSessionId: session.id, status: 'stopped', cleanup: 'complete'
  })
  assert.equal((await client.listAppSessions())[0]?.status, 'stopped')
  await assert.rejects(client.act({ type: 'click', appSessionId: session.id, snapshotId: snapshot.id, elementId: 'button' }), hasCode('APP_SESSION_STOPPED', 'none'))
  assert.equal((await client.listApps())[0]?.name, 'clicks:0')
})

test('an app stop acknowledgement for another app fails the connection', async (t) => {
  const { client } = await start(t, 'wrong-app-stop')
  await assert.rejects(client.stopAppSession({ appSessionId: 'control' }), hasCode('PROTOCOL_ERROR'))
  await assert.rejects(client.listApps(), hasCode('CLOSED'))
})

test('a snapshot for another app session is rejected before the host can use it', async (t) => {
  const { client } = await start(t, 'foreign-snapshot')
  await assert.rejects(client.getAppState({ appSessionId: 'control' }), hasCode('PROTOCOL_ERROR'))
  await assert.rejects(client.listApps(), hasCode('CLOSED'))
})

test('unconfirmed app cleanup disables further calls and preserves cleanup failure', async (t) => {
  const { client } = await start(t, 'stop-failure')
  await assert.rejects(client.stopAppSession({ appSessionId: 'control' }), error => {
    assert.ok(error instanceof ComputerUseError)
    assert.equal(error.code, 'CLEANUP_FAILED')
    assert.equal(error.cleanup, 'unconfirmed')
    assert.equal(error.effect, 'possible')
    return true
  })
  await assert.rejects(client.listApps(), hasCode('CLOSED'))
})

test('observations and actions preserve structured Unicode data and decoded images over fragmented frames', async (t) => {
  const { client } = await start(t)
  const [apps, permissions, capabilities, snapshot] = await Promise.all([
    client.listApps(),
    client.getPermissionStatus(),
    client.getCapabilities(),
    client.getAppState({ appSessionId: 'control' })
  ])
  assert.equal(apps[0]?.id, 'app')
  assert.deepEqual(permissions, { permissions: [] })
  assert.equal(capabilities.capabilities[0]?.name, 'click')
  assert.equal(snapshot.app.name, '测试 🙂')
  assert.equal(snapshot.screenshot.status, 'available')
  if (snapshot.screenshot.status !== 'available')
    assert.fail('Expected screenshot')
  assert.ok(snapshot.screenshot.image.data instanceof Uint8Array)
  assert.deepEqual(
    [...snapshot.screenshot.image.data.subarray(0, 8)],
    [137, 80, 78, 71, 13, 10, 26, 10]
  )
  assert.equal('dataBase64' in snapshot.screenshot.image, false)
  const result = await client.act({
    type: 'click', appSessionId: 'control',
    snapshotId: snapshot.id,
    elementId: 'button'
  })
  assert.equal(result.observation.status, 'available')
  if (result.observation.status !== 'available')
    assert.fail('Expected updated observation')
  assert.notEqual(result.observation.snapshot.id, snapshot.id)
  assert.deepEqual(await client.listApps(), [{ id: 'app', name: 'clicks:1' }])
})

test('permission onboarding survives the ordinary 30-second deadline and returns observed state on dismissal', { timeout: 40_000 }, async (t) => {
  const { client, child } = await start(t, 'permission-wait')
  const opened = once(child, 'message')
  const request = client.requestPermissions({ ids: ['screenRecording'] })
  let settled = false
  void request.then(() => { settled = true }, () => { settled = true })
  assert.equal((await opened)[0], 'permission-opened')
  await delay(31_000)
  assert.equal(settled, false)
  child.send('dismiss')
  assert.deepEqual(await request, {
    permissions: [{ id: 'screenRecording', label: 'screenRecording', status: 'unknown', interaction: 'systemSettings' }]
  })
})

for (const reason of ['abort', 'timeout'] as const) {
  test(`permission ${reason} waits for onboarding cleanup`, async (t) => {
    const { client, child } = await start(t, 'permission-wait')
    const controller = new AbortController()
    const opened = once(child, 'message')
    const request = client.requestPermissions({ ids: ['accessibility'] }, {
      signal: controller.signal,
      ...(reason === 'timeout' ? { timeoutMs: 250 } : {})
    })
    const rejection = assert.rejects(request, hasCode(reason === 'timeout' ? 'TIMEOUT' : 'CANCELLED', 'possible'))
    let settled = false
    void request.then(() => { settled = true }, () => { settled = true })
    assert.equal((await opened)[0], 'permission-opened')
    const cancelled = once(child, 'message')
    if (reason === 'abort') controller.abort()
    assert.equal((await cancelled)[0], 'permission-cancelled')
    await setImmediate()
    assert.equal(settled, false)
    child.send('window-closed')
    await rejection
    assert.equal((await client.listApps())[0]?.name, 'clicks:0')
  })
}

test('invalid actions are rejected before dispatch and the client remains usable', async (t) => {
  const { client } = await start(t)
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  for (const params of [
    { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button', x: 1, y: 1 },
    { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, x: NaN, y: 1 },
    { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, x: 1 },
    { type: 'setValue', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' }
  ])
    await assert.rejects(
      client.act(params as Action),
      hasCode('INVALID_ARGUMENT', 'none')
    )
  await assert.rejects(
    client.listApps({ timeoutMs: 0 }),
    hasCode('INVALID_ARGUMENT')
  )
  assert.deepEqual(await client.listApps(), [{ id: 'app', name: 'clicks:0' }])
})

test('stale and cross-client snapshot errors are preserved without retrying', async (t) => {
  const first = await start(t)
  const second = await start(t)
  const old = await first.client.getAppState({ appSessionId: 'control' })
  await first.client.getAppState({ appSessionId: 'control' })
  const action: Action = {
    type: 'click', appSessionId: 'control',
    snapshotId: old.id,
    elementId: 'button'
  }
  await assert.rejects(
    first.client.act(action),
    hasCode('STALE_SNAPSHOT', 'none')
  )
  await assert.rejects(
    second.client.act(action),
    hasCode('STALE_SNAPSHOT', 'none')
  )
  assert.equal((await first.client.listApps())[0]?.name, 'clicks:0')
  assert.equal((await second.client.listApps())[0]?.name, 'clicks:0')
})

test('post-action capture failure is returned as a completed action and never retried', async (t) => {
  const { client } = await start(t, 'capture-failure')
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  const result = await client.act({
    type: 'click', appSessionId: 'control',
    snapshotId: snapshot.id,
    elementId: 'button'
  })
  assert.deepEqual(result, {
    status: 'completed',
    observation: {
      status: 'unavailable',
      reason: { code: 'CAPTURE_FAILED', message: 'Screenshot failed' }
    }
  })
  assert.equal((await client.listApps())[0]?.name, 'clicks:1')
})

test('remote action errors retain confirmed effects', async (t) => {
  const { client } = await start(t, 'error-after-effect')
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  await assert.rejects(
    client.act({ type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' }),
    hasCode('CAPTURE_FAILED', 'applied')
  )
  assert.equal((await client.listApps())[0]?.name, 'clicks:1')
})

for (const reason of ['abort', 'timeout', 'close'] as const) {
  test(`${reason} waits for native cancellation acknowledgement`, async (t) => {
    const { client, child } = await start(t, 'cancel')
    const snapshot = await client.getAppState({ appSessionId: 'control' })
    const controller = new AbortController()
    const begun = once(child, 'message')
    const action = client.act(
      { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' },
      {
        signal: controller.signal,
        timeoutMs: reason === 'timeout' ? 250 : 5_000
      }
    )
    const rejection = assert.rejects(
      action,
      hasCode(reason === 'timeout' ? 'TIMEOUT' : 'CANCELLED', 'none')
    )
    let settled = false
    void action.then(
      () => {
        settled = true
      },
      () => {
        settled = true
      }
    )
    assert.equal((await begun)[0], 'action-started')
    const cancelled = once(child, 'message')
    const closing = reason === 'close' ? client.close() : undefined
    if (reason === 'abort') controller.abort()
    assert.equal((await cancelled)[0], 'cancel-received')
    await setImmediate()
    assert.equal(settled, false)
    child.send('confirm-cancel')
    await rejection
    if (closing) await closing
    else assert.equal((await client.listApps())[0]?.name, 'clicks:0')
  })
}

test('a pre-aborted action never reaches the runtime', async (t) => {
  const { client } = await start(t)
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  await assert.rejects(
    client.act(
      { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' },
      { signal: AbortSignal.abort() }
    ),
    hasCode('CANCELLED', 'none')
  )
  assert.equal((await client.listApps())[0]?.name, 'clicks:0')
})

test('native completion can win a cancellation race without replay', async (t) => {
  const { client, child } = await start(t, 'late-completion')
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  const controller = new AbortController()
  const begun = once(child, 'message')
  const action = client.act(
    { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' },
    { signal: controller.signal }
  )
  await begun
  controller.abort()
  assert.equal((await action).status, 'completed')
  assert.equal((await client.listApps())[0]?.name, 'clicks:1')
})

test('unacknowledged cancellation disables the client and reports uncertain effects', async (t) => {
  const { client, child } = await start(t, 'ignore-cancel')
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  const controller = new AbortController()
  const begun = once(child, 'message')
  const action = client.act(
    { type: 'click', appSessionId: 'control', snapshotId: snapshot.id, elementId: 'button' },
    { signal: controller.signal }
  )
  const rejection = assert.rejects(action, hasCode('CANCELLED', 'possible'))
  await begun
  controller.abort()
  await rejection
  await assert.rejects(client.listApps(), hasCode('CLOSED'))
  await assert.rejects(client.close(), hasCode('CLEANUP_FAILED'))
  assert.ok(child.exitCode !== null || child.signalCode !== null)
})

for (const mode of [
  'mismatch',
  'wrong-owner',
  'bootstrap-exit',
  'bootstrap-hang'
]) {
  test(`startup cleans up after ${mode}`, async (t) => {
    const { child, connection, sessionId } = launch(t, mode)
    await assert.rejects(
      connectClient(connection, sessionId, { timeoutMs: mode === 'bootstrap-hang' ? 250 : 5_000 }),
      hasCode(
        mode === 'bootstrap-exit'
          ? 'RUNTIME_EXITED'
          : mode === 'bootstrap-hang'
            ? 'TIMEOUT'
            : 'PROTOCOL_MISMATCH'
      )
    )
    assert.ok(child.exitCode !== null || child.signalCode !== null)
  })
}

for (const mode of [
  'malformed',
  'null-envelope',
  'ambiguous-envelope',
  'exit',
  'eof'
]) {
  test(`${mode} response closes the client and releases its process`, async (t) => {
    const { client, child } = await start(t, mode)
    await assert.rejects(
      client.getAppState({ appSessionId: 'control' }),
      hasCode(
        mode === 'exit' || mode === 'eof' ? 'RUNTIME_EXITED' : 'PROTOCOL_ERROR'
      )
    )
    await assert.rejects(client.close(), hasCode('CLEANUP_FAILED'))
    assert.ok(child.exitCode !== null || child.signalCode !== null)
  })
}

test('a broken input pipe rejects an in-flight write without an unhandled rejection', async (t) => {
  const { client, child } = await start(t, 'write-failure')
  const inputClosed = once(child, 'message')
  const snapshot = await client.getAppState({ appSessionId: 'control' })
  child.send('close-input')
  assert.equal((await inputClosed)[0], 'input-closed')
  await assert.rejects(
    client.act({
      type: 'typeText', appSessionId: 'control',
      snapshotId: snapshot.id,
      text: 'x'.repeat(100_000)
    }),
    hasCode('PROTOCOL_ERROR', 'possible')
  )
  await assert.rejects(client.close(), hasCode('CLEANUP_FAILED'))
})

test(
  'a blocked write cannot keep cancellation and process cleanup pending indefinitely',
  { timeout: 10_000 },
  async (t) => {
    const { client, child } = await start(t, 'backpressure')
    const inputBlocked = once(child, 'message')
    const snapshot = await client.getAppState({ appSessionId: 'control' })
    child.send('pause-input')
    await inputBlocked
    await assert.rejects(
      client.act(
        {
          type: 'typeText', appSessionId: 'control',
          snapshotId: snapshot.id,
          text: 'x'.repeat(8_000_000)
        },
        { timeoutMs: 100 }
      ),
      hasCode('TIMEOUT', 'possible')
    )
    await assert.rejects(client.close(), hasCode('CLEANUP_FAILED'))
    assert.ok(child.exitCode !== null || child.signalCode !== null)
  }
)

test('close is idempotent, reports confirmed cleanup, and leaves another client running', async (t) => {
  const first = await start(t)
  const second = await start(t)
  let removedCalled = false
  first.client.onClosed(() => {
    removedCalled = true
  })()
  const event = Promise.withResolvers<unknown>()
  first.client.onClosed(event.resolve)
  const closing = first.client.close()
  assert.equal(first.client.close(), closing)
  await closing
  assert.deepEqual(await event.promise, { cleanup: 'complete' })
  assert.equal(removedCalled, false)
  assert.equal(first.child.exitCode, 0)
  assert.equal((await second.client.listApps())[0]?.id, 'app')
  await assert.rejects(first.client.listApps(), hasCode('CLOSED'))
})

for (const mode of ['shutdown-hang', 'wrong-shutdown-owner']) {
  test(`${mode} cannot be reported as successful cleanup`, async (t) => {
    const { client, child } = await start(t, mode)
    await assert.rejects(client.close(), hasCode('CLEANUP_FAILED'))
    assert.ok(child.exitCode !== null || child.signalCode !== null)
  })
}

test('spawn failure is surfaced without leaving an unusable connection alive', async (t) => {
  const sessionId = randomUUID()
  const child = spawn(join(tmpdir(), `missing-${sessionId}`), [], {
    stdio: 'pipe'
  })
  const connection = new RuntimeConnection(child, sessionId)
  t.after(() => connection.close().catch(() => {}))
  await assert.rejects(
    connectClient(connection, sessionId),
    hasCode('RUNTIME_START_FAILED')
  )
})

test('public start checks cancellation before runtime discovery', async () => {
  await assert.rejects(
    ComputerUse.start({}, { signal: AbortSignal.abort() }),
    hasCode('CANCELLED')
  )
  await assert.rejects(
    ComputerUse.start({ runtimePath: '' }),
    hasCode('INVALID_ARGUMENT')
  )
  await assert.rejects(
    ComputerUse.start({ runtimePath: join(tmpdir(), randomUUID()) }),
    hasCode('RUNTIME_NOT_FOUND')
  )
})

test(
  'public start launches an explicit path containing spaces with the private session arguments',
  { skip: process.platform === 'win32' },
  async (t) => {
    const directory = await mkdtemp(join(tmpdir(), 'computer use '))
    t.after(() => rm(directory, { recursive: true, force: true }))
    const runtimePath =
      process.platform === 'darwin'
        ? join(directory, 'Test.app')
        : join(directory, 'test runtime')
    const executable =
      process.platform === 'darwin'
        ? join(runtimePath, 'Contents', 'MacOS', 'OpenComputerUse')
        : runtimePath
    await mkdir(join(executable, '..'), { recursive: true })
    await writeFile(
      executable,
      `#!${process.execPath}\nimport(${JSON.stringify(fixture.href)})\n`
    )
    await chmod(executable, 0o755)
    const client = await ComputerUse.start({ runtimePath })
    t.after(() => client.close())
    assert.equal((await client.listApps())[0]?.id, 'app')
    await client.close()
  }
)
