import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { ComputerUse } from '../packages/sdk/dist/index.js'

// Start apps/OpenComputerUseLinux/testdata/gtk_fixture.py in the desktop session first.

function shellDump() {
  const js = `JSON.stringify((() => { const [x, y] = global.get_pointer(); return { pointer: [x, y], overview: Main.overview.visible,
    at: Main.extensionManager.lookup('computer-use@cherryai').stateObj._describeAt(x, y),
    windows: global.get_window_actors().map(a => a.meta_window).filter(Boolean).map(w => { const f = w.get_frame_rect(), b = w.get_buffer_rect();
      return [w.get_title(), [f.x, f.y, f.width, f.height], [b.x, b.y, b.width, b.height], w.has_focus()] }) } })())`
  return spawnSync('gdbus', ['call', '--session', '--dest', 'org.gnome.Shell', '--object-path', '/org/gnome/Shell',
    '--method', 'org.gnome.Shell.Eval', js], { encoding: 'utf8' }).stdout
}

// Allows desktop control in the GNOME Shell extension's dialog once it opens.
// Shell.Eval exists only in the isolated test session's unsafe mode.
async function answerShellDialog() {
  for (let attempt = 0; attempt < 100; attempt++) {
    await delay(100)
    const answer = spawnSync('gdbus', ['call', '--session', '--dest', 'org.gnome.Shell', '--object-path', '/org/gnome/Shell',
      '--method', 'org.gnome.Shell.Eval', "Main.extensionManager.lookup('computer-use@cherryai').stateObj._answerForTest(true)"], { encoding: 'utf8' })
    if (answer.stdout.includes("'true'")) return
  }
  throw new Error('The GNOME Shell extension never asked for desktop control')
}
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

  const session_type = process.env.XDG_SESSION_TYPE
  const wayland = session_type ? session_type === 'wayland' : Boolean(process.env.WAYLAND_DISPLAY)
  const capabilities = (await client.getCapabilities()).capabilities
  const keyboard = capabilities.find(item => item.name === 'pressKey').availability
  // X11 and wlroots compositors (virtual pointer, compositor IPC) offer the pointer.
  const pointer = capabilities.find(item => item.name === 'drag').availability
  const screenshot = capabilities.find(item => item.name === 'screenshot').availability
  // On GNOME the Shell extension serves screenshots and input once the user allows it.
  const shellGrant = () => client.getPermissionStatus().then(status => status.permissions.find(permission => permission.id === 'gnomeShell'))
  const shellPending = (await shellGrant())?.status === 'notDetermined'
  if (shellPending) {
    assert.equal(snapshot.screenshot.status, 'unavailable')
    assert.equal(snapshot.screenshot.reason.code, 'PERMISSION_REQUIRED')
  } else {
    assert.equal(snapshot.screenshot.status, screenshot.status === 'available' ? 'available' : 'unavailable', JSON.stringify(snapshot.screenshot))
    if (screenshot.status === 'available') assert.equal(snapshot.screenshot.image.mimeType, 'image/png')
  }
  const pointerActions = [
    { type: 'drag', from: { x: 20, y: 20 }, to: { x: 60, y: 20 } },
    { type: 'click', x: 20, y: 20 },
  ]
  const keyActions = [
    { type: 'pressKey', key: 'Return' },
    { type: 'scroll', direction: 'down', pages: 1 },
  ]
  const global = action => client.act({ appSessionId: session.id, snapshotId: snapshot.id, ...action, allowGlobalInput: true })
  // Without allowGlobalInput nothing global is sent.
  for (const action of [...pointerActions, ...keyActions]) {
    await state(() => true)
    const availability = pointerActions.includes(action) ? pointer : keyboard
    const expected = availability.status === 'available' ? 'PERMISSION_REQUIRED' : availability.reason.code
    await assert.rejects(act(action), refused(expected))
  }
  if (pointer.status !== 'available') {
    for (const action of pointerActions) {
      await state(() => true)
      await assert.rejects(global(action), refused(pointer.reason.code))
    }
  }
  if (wayland) {
    // GNOME with the Shell extension lists gnomeShell, KDE kdeIntegration; portal
    // sessions (GNOME or KDE without them) list remoteDesktop; the wlroots virtual
    // keyboard needs no grant.
    const permissions = (await client.getPermissionStatus()).permissions
    const portal = permissions.some(permission => permission.id === 'remoteDesktop')
    if (keyboard.status !== 'available') return
    if (shellPending) {
      // Keys, clicks and screenshots wait for desktop control. The Shell's dialog is
      // answered through Shell.Eval, which only isolated test sessions serve.
      await state(() => true)
      await assert.rejects(global({ type: 'pressKey', key: 'Return' }), refused('PERMISSION_REQUIRED'))
      if (process.env.OPEN_COMPUTER_USE_WAYLAND_TEST !== '1') return
      const answering = answerShellDialog()
      const granted = await client.requestPermissions({ ids: ['gnomeShell'] })
      await answering
      assert.equal(granted.permissions.find(permission => permission.id === 'gnomeShell').status, 'granted')
      await state(() => true)
      assert.equal(snapshot.screenshot.status, 'available', JSON.stringify(snapshot.screenshot))
    } else if (portal) {
      // Keys wait for the remoteDesktop permission. The grant needs the portal dialog,
      // so it only runs where the session can answer it (testdata/gnome-session.sh).
      await state(() => true)
      await assert.rejects(global({ type: 'pressKey', key: 'Return' }), refused('PERMISSION_REQUIRED'))
      if (process.env.OPEN_COMPUTER_USE_WAYLAND_TEST !== '1') return
      const granted = await client.requestPermissions({ ids: ['remoteDesktop'] })
      assert.equal(granted.permissions.find(permission => permission.id === 'remoteDesktop').status, 'granted')
      // GNOME without the Shell extension says how to get it.
      const shell = granted.permissions.find(permission => permission.id === 'gnomeShell')
      if (shell) assert.deepEqual([shell.status, shell.interaction], ['unavailable', 'systemSettings'])
    } else {
      // wlroots needs no grant; KDE's installed integration is already granted.
      assert.deepEqual(permissions.filter(permission => permission.status !== 'granted'), [])
      assert.ok(permissions.every(permission => permission.id === 'kdeIntegration'), JSON.stringify(permissions))
    }
  }
  // With explicit permission, keys reach the focused fixture field.
  await state(() => true)
  await global({ type: 'pressKey', key: 'ctrl+a' })
  await state(() => true)
  await global({ type: 'pressKey', key: 'X' })
  await state(elements => elements.some(element => element.name === 'Second field' && element.value === 'X'))
  await state(() => true)
  assert.equal((await global({ type: 'scroll', direction: 'down', pages: 1 })).status, 'completed')
  if (pointer.status !== 'available') return
  // Pointer: a double click presses the counter twice; points outside the window are refused.
  await state(() => true)
  await global({ type: 'click', elementId: named(`Count: ${count + 1}`).id, count: 2 })
  try {
    await state(elements => elements.some(element => element.name === `Count: ${count + 3}`))
  } catch (error) {
    // Where the pointer and windows were, as the Shell sees them (test sessions only).
    if (shellPending) console.error('Shell state after the double click:', shellDump())
    throw error
  }
  await assert.rejects(global({ type: 'click', x: 5000, y: 5000 }), refused('INVALID_ARGUMENT'))
})
