import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { ComputerUse } from '../packages/sdk/dist/index.js'

// Start apps/OpenComputerUseLinux/testdata/gtk_fixture.py in the desktop session first.
const fixtureName = process.env.COMPUTER_USE_LINUX_ACTION_FIXTURE

test('Linux SDK engine runs semantic actions and refuses global input', {
  skip: process.platform !== 'linux' || !fixtureName, timeout: 120000
}, async t => {
  const client = await ComputerUse.start({ runtimePath: process.env.COMPUTER_USE_RUNTIME_PATH })
  t.after(() => client.close().catch(() => {}))
  let app
  for (let attempt = 0; attempt < 30 && !app; attempt++) {
    app = (await client.listApps()).find(app => app.name === fixtureName)
    if (!app) await delay(100)
  }
  assert.ok(app, 'start the Linux GTK fixture in the same desktop session')
  const session = await client.openAppSession({ appId: app.id })
  let snapshot
  async function state(predicate) {
    for (let attempt = 0; attempt < 20; attempt++) {
      snapshot = await client.getAppState({ appSessionId: session.id })
      assert.equal(snapshot.tree.status, 'available')
      if (predicate(snapshot.tree.elements)) return
      await delay(100)
    }
    assert.fail(`Fixture state did not converge: ${JSON.stringify(snapshot.tree)}`)
  }
  const named = name => snapshot.tree.elements.find(element => element.name === name)
  const act = action => client.act({ ...action, appSessionId: session.id, snapshotId: snapshot.id, allowGlobalInput: false })
  const refused = code => error => error.code === code && error.effect === 'none'

  await state(elements => elements.some(element => element.name.startsWith('Count: ')))
  const count = Number(snapshot.tree.elements.find(element => element.name.startsWith('Count: ')).name.slice(7))
  const button = named(`Count: ${count}`)
  const secondary = button.secondaryActions.find(action => action.label === 'click')
  assert.ok(secondary, JSON.stringify(button))
  assert.equal((await act({ type: 'performSecondaryAction', elementId: button.id, actionId: secondary.id })).status, 'completed')
  await state(elements => elements.some(element => element.name === `Count: ${count + 1}`))

  assert.ok(named('Level').actions.includes('setValue'))
  assert.ok(!named('Locked field').actions.includes('setValue'), 'read-only text must not offer setValue')
  await act({ type: 'setValue', elementId: named('Level').id, value: '7' })
  await state(elements => elements.some(element => element.name === 'Level' && element.value === '7'))
  await assert.rejects(act({ type: 'setValue', elementId: named('Level').id, value: '11' }), refused('INVALID_ARGUMENT'))

  await state(() => true)
  await act({ type: 'setValue', elementId: named('First field').id, value: '中文 😀' })
  await state(elements => elements.some(element => element.name === 'First field' && element.value === '中文 😀'))
  await act({ type: 'setValue', elementId: named('Second field').id, value: '' })
  await state(elements => elements.some(element => element.name === 'Second field' && (element.value ?? '') === ''))
  // The fixture focuses the second field; typing never chooses another field.
  await act({ type: 'typeText', text: '追加🙂' })
  await state(elements => elements.some(element => element.name === 'Second field' && element.value === '追加🙂'))
  assert.equal(named('First field').value, '中文 😀')

  for (const action of [
    { type: 'pressKey', key: 'Return', allowGlobalInput: true },
    { type: 'scroll', direction: 'down', pages: 1 },
    { type: 'drag', from: { x: 20, y: 20 }, to: { x: 60, y: 20 } },
    { type: 'click', x: 20, y: 20 },
  ]) {
    await state(() => true)
    await assert.rejects(client.act({ appSessionId: session.id, snapshotId: snapshot.id, allowGlobalInput: false, ...action }), refused('UNSUPPORTED_CAPABILITY'))
  }
  await state(elements => elements.some(element => element.name === `Count: ${count + 1}`))
})
