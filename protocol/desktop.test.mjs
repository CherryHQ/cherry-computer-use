import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { ComputerUse } from '../packages/sdk/dist/index.js'

const runtimePath = process.env.COMPUTER_USE_RUNTIME_PATH
const fixtureName = process.env.COMPUTER_USE_FIXTURE_APP

test('SDK observes and clicks a real fixture, rejects stale and foreign identities', { skip: !fixtureName, timeout: 90000 }, async t => {
  const first = await ComputerUse.start({ runtimePath })
  t.after(() => first.close().catch(() => {}))
  const second = await ComputerUse.start({ runtimePath })
  t.after(() => second.close().catch(() => {}))
  async function target(client) {
    let apps = []
    for (let attempt = 0; attempt < 30; attempt++) {
      apps = await client.listApps()
      const app = apps.find(app => app.name === fixtureName)
      if (app) return (await client.openAppSession({ appId: app.id })).id
      await delay(100)
    }
    assert.fail(`Fixture ${fixtureName} is not discoverable: ${JSON.stringify(apps)}`)
  }
  async function counter(client, appSessionId, count) {
    for (let attempt = 0; attempt < 30; attempt++) {
      const snapshot = await client.getAppState({ appSessionId })
      const element = snapshot.tree.status === 'available' && snapshot.tree.elements.find(element => element.name === `Count: ${count}` && element.actions.includes('click'))
      if (element) return { snapshot, element }
      await delay(100)
    }
    assert.fail(`Fixture did not reach Count: ${count}`)
  }
  const firstApp = await target(first)
  const secondApp = await target(second)
  assert.notEqual(firstApp, secondApp, 'app IDs must belong to a private session')
  await assert.rejects(second.getAppState({ appSessionId: firstApp }), error => error.code === 'APP_SESSION_NOT_FOUND')
  const initial = await counter(first, firstApp, 0)
  const tree = initial.snapshot.tree.elements
  const ids = new Set(tree.map(element => element.id))
  assert.ok(tree.some(element => element.parentId), 'tree must expose hierarchy')
  assert.ok(tree.every(element => !element.parentId || ids.has(element.parentId)))
  if (process.env.COMPUTER_USE_REQUIRE_SCREENSHOT === '1') {
    const capture = initial.snapshot.screenshot
    assert.equal(capture.status, 'available', JSON.stringify(capture))
    assert.deepEqual([...capture.image.data.slice(0, 8)], [137,80,78,71,13,10,26,10])
    assert.ok(capture.image.width >= 100 && capture.image.height >= 100)
  }
  const click = ({ snapshot, element }) => ({ type: 'click', appSessionId: snapshot.appSessionId, snapshotId: snapshot.id, elementId: element.id })
  await assert.rejects(second.act(click(initial)), error => error.code === 'APP_SESSION_NOT_FOUND' && error.effect === 'none')
  const completed = await first.act(click(initial))
  assert.equal(completed.status, 'completed')
  await assert.rejects(first.act(click(initial)), error => error.code === 'STALE_SNAPSHOT')
  const stale = await counter(first, firstApp, 1)
  const fresh = await counter(second, secondApp, 1)
  assert.equal((await second.act(click(fresh))).status, 'completed')
  await counter(second, secondApp, 2)
  await assert.rejects(first.act(click(stale)), error => error.code === 'STALE_SNAPSHOT' && error.effect === 'none')
  await counter(first, firstApp, 2)
  const limited = await first.getAppState({ appSessionId: firstApp, maxTreeNodes: 1 })
  assert.equal(limited.tree.status, 'available')
  assert.equal(limited.tree.elements.length, 1)
  assert.ok(limited.tree.truncated.includes('nodes'))
  const stopped = await first.stopAppSession({ appSessionId: firstApp })
  assert.equal(stopped.cleanup, 'complete')
  assert.equal((await first.listAppSessions()).find(session => session.id === firstApp).status, 'stopped')
  await assert.rejects(first.getAppState({ appSessionId: firstApp }), error => error.code === 'APP_SESSION_STOPPED')
  await assert.rejects(first.act(click(stale)), error => error.code === 'APP_SESSION_STOPPED')
  await first.close()
  await counter(second, secondApp, 2)
  await second.close()
})
