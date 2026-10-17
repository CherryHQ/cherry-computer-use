import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ComputerUse } from '../packages/sdk/dist/index.js'

const query = process.env.COMPUTER_USE_LAUNCH_APP

test('installed app cold launch validates identity and waits for its window', {
  skip: !query, timeout: 90000
}, async t => {
  const client = await ComputerUse.start({ runtimePath: process.env.COMPUTER_USE_RUNTIME_PATH })
  t.after(() => client.close().catch(() => {}))
  const identity = await client.resolveAppIdentity({ query })
  assert.ok(identity.id)
  const initial = await client.listApps()
  assert.ok(!initial.some(app => app.identity === identity.id), 'cold-launch fixture must not already be running')
  assert.equal((await client.resolveAppIdentity({ query: identity.query })).id, identity.id)
  await assert.rejects(client.resolveApp({ query: identity.query, expectedIdentity: 'different.application' }), {
    code: 'TARGET_UNAVAILABLE', effect: 'none'
  })
  await assert.rejects(client.resolveApp({ query: identity.query, launch: false }), {
    code: 'TARGET_UNAVAILABLE', effect: 'none'
  })
  assert.ok(!(await client.listApps()).some(app => app.identity === identity.id), 'preflight must not launch')
  const app = await client.resolveApp({ query: identity.query, expectedIdentity: identity.id })
  assert.equal(app.identity, identity.id)
  const control = await client.openAppSession({ appId: app.id })
  assert.equal(control.app.identity, identity.id)
  const snapshot = await client.getAppState({ appSessionId: control.id, includeScreenshot: false })
  assert.equal(snapshot.tree.status, 'available')
  assert.ok(snapshot.tree.elements.length > 0)
  assert.equal((await client.resolveApp({ query: identity.query, launch: false })).id, app.id)
  assert.equal((await client.listApps()).find(item => item.id === app.id).identity, identity.id)
})
