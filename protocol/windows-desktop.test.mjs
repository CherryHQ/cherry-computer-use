import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { ComputerUse } from '../packages/sdk/dist/index.js'

const fixtureName = process.env.COMPUTER_USE_WINDOWS_ACTION_FIXTURE

test('Windows SDK engine handles Chinese targets and all seven actions', {
  skip: process.platform !== 'win32' || !fixtureName, timeout: 180000
}, async t => {
  const client = await ComputerUse.start({ runtimePath: process.env.COMPUTER_USE_RUNTIME_PATH })
  t.after(() => client.close().catch(() => {}))
  let app
  for (let attempt = 0; attempt < 20 && !app; attempt++) {
    app = (await client.listApps()).find(app => app.name === fixtureName)
    if (!app) await delay(100)
  }
  assert.ok(app, 'start the Windows actions fixture in the same interactive session')
  assert.equal((await client.listApps()).find(item => item.name === fixtureName).id, app.id)
  const session = await client.openAppSession({ appId: app.id })
  let snapshot
  async function state(predicate) {
    for (let attempt = 0; attempt < 10; attempt++) {
      snapshot = await client.getAppState({ appSessionId: session.id })
      assert.equal(snapshot.tree.status, 'available')
      if (predicate(snapshot.tree.elements)) return
      await delay(100)
    }
    assert.fail(`Fixture state did not converge: ${JSON.stringify(snapshot.tree)}`)
  }
  const named = name => snapshot.tree.elements.find(element => element.name === name)
  async function act(action) {
    const result = await client.act({ ...action, appSessionId: session.id, snapshotId: snapshot.id, allowGlobalInput: false })
    assert.equal(result.status, 'completed')
  }
  await state(elements => elements.some(element => element.name === '显示侧边栏'))
  snapshot = await client.getAppState({ appSessionId: session.id, textLimit: 2 })
  assert.ok(snapshot.tree.truncated.includes('text'))
  const shortened = snapshot.tree.elements.find(element => element.name.startsWith('显示'))
  assert.ok(shortened)
  await act({ type: 'click', elementId: shortened.id })
  await state(elements => elements.some(element => element.name === 'Toggle: on'))
  const toggle = named('显示侧边栏')
  const secondary = toggle.secondaryActions.find(action => action.label === 'Toggle')
  assert.ok(secondary)
  await act({ type: 'performSecondaryAction', elementId: toggle.id, actionId: secondary.id })
  await state(elements => elements.some(element => element.name === 'Toggle: off'))
  await act({ type: 'setValue', elementId: named('Editor').id, value: '中文 😀' })
  await state(elements => elements.some(element => element.name === 'Editor' && element.value === '中文 😀'))
  await act({ type: 'setValue', elementId: named('Editor').id, value: '' })
  await state(elements => elements.some(element => element.name === 'Editor' && (element.value ?? '') === ''))
  await act({ type: 'typeText', text: '中文 😀追加' })
  await state(elements => elements.some(element => element.name === 'Editor' && element.value === '中文 😀追加'))
  await act({ type: 'pressKey', key: 'Return' })
  await state(elements => elements.some(element => element.name === 'Events: clicks=0 wheels=0 keys=1 drags=0'))
  await act({ type: 'scroll', direction: 'down', pages: 1 })
  await state(elements => elements.some(element => element.name === 'Events: clicks=0 wheels=1 keys=1 drags=0'))
  await act({ type: 'drag', from: { x: 20, y: 160 }, to: { x: 100, y: 160 } })
  await state(elements => elements.some(element => element.name === 'Events: clicks=1 wheels=1 keys=1 drags=12'))
  await act({ type: 'click', x: 20, y: 160, count: 2 })
  await state(elements => elements.some(element => element.name === 'Events: clicks=3 wheels=1 keys=1 drags=12'))
  await act({ type: 'setValue', elementId: named('Editor').id, value: '' })
  await state(elements => elements.some(element => element.name === 'Editor' && (element.value ?? '') === ''))
})
