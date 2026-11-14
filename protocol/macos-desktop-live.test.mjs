import assert from 'node:assert/strict'
import { execFileSync, spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { test } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { ComputerUse } from '../packages/sdk/dist/index.js'

const enabled = process.platform === 'darwin' && process.env.OPEN_COMPUTER_USE_RUN_SDK_DESKTOP_LIVE_TEST === '1'
const runtimePath = resolve(process.env.COMPUTER_USE_RUNTIME_PATH ?? 'dist/Cherry Computer Use.app')

test('macOS public SDK captures a native window and edits its text through the app agent', {
  skip: !enabled, timeout: 90000
}, async t => {
  const root = await mkdtemp(join(tmpdir(), 'cherry-sdk-desktop-'))
  let computer
  let fixture
  let fixtureExit
  t.after(async () => {
    // Close the automation owner before terminating its target.
    try { await computer?.close() } finally {
      if (fixture && fixture.exitCode === null && fixture.signalCode === null) {
        fixture.kill('SIGTERM')
        if (!await Promise.race([fixtureExit.then(() => true), delay(3000).then(() => false)])) {
          fixture.kill('SIGKILL')
          await fixtureExit
        }
      }
      await rm(root, { recursive: true, force: true })
    }
  })
  computer = await ComputerUse.start({ runtimePath })
  const permissions = await computer.getPermissionStatus()
  assert.ok(permissions.permissions.every(permission => permission.status === 'granted'), JSON.stringify(permissions))
  const binary = join(root, `CherrySDKFixture${process.pid}`)
  const statePath = join(root, 'state.json')
  const commandPath = join(root, 'command.json')
  execFileSync('xcrun', ['swiftc', 'scripts/tests/fixtures/MacOSTextInputFixture.swift', '-o', binary], { stdio: 'pipe' })
  fixture = spawn(binary, [statePath, commandPath], { stdio: 'ignore' })
  fixtureExit = once(fixture, 'exit')
  async function waitForState(predicate) {
    const deadline = Date.now() + 5000
    let state
    while (Date.now() < deadline) {
      try { state = JSON.parse(await readFile(statePath, 'utf8')) } catch {}
      if (state && predicate(state)) return state
      assert.equal(fixture.exitCode, null, 'fixture must remain running')
      await delay(30)
    }
    assert.fail(`Fixture state did not reach the expected result: ${JSON.stringify(state)}`)
  }
  await waitForState(state => state.windowID > 0)
  await writeFile(commandPath, JSON.stringify({ serial: 1, reset: true }))
  const initial = await waitForState(state => state.serial === 1)
  const app = await computer.resolveApp({ query: `CherrySDKFixture${process.pid}`, launch: false })
  const session = await computer.openAppSession({ appId: app.id })
  const target = { appSessionId: session.id, windowId: String(initial.windowID), activation: 'never' }

  const ax = await computer.getAppState({ ...target, includeScreenshot: false })
  assert.equal(ax.window.id, target.windowId)
  assert.equal(ax.tree.status, 'available')
  assert.ok(ax.tree.elements.some(element => element.value === 'hello world'))
  assert.equal(ax.screenshot.status, 'unavailable')
  assert.equal(ax.screenshot.reason.code, 'CAPTURE_NOT_REQUESTED')

  const imagePaths = []
  for (const format of ['png', 'jpeg', 'webp']) {
    const snapshot = await computer.getAppState({ ...target, includeAX: false,
      screenshot: { format, maxDimension: 640, ...(format === 'jpeg' ? { jpegQuality: 0.8 } : {}) } })
    assert.equal(snapshot.tree.status, 'unavailable')
    assert.equal(snapshot.tree.reason.code, 'OBSERVATION_NOT_REQUESTED')
    assert.equal(snapshot.screenshot.status, 'available', JSON.stringify(snapshot.screenshot))
    const image = snapshot.screenshot.image
    assert.equal(image.mimeType, `image/${format}`)
    assert.ok(image.width > 0 && image.height > 0 && Math.max(image.width, image.height) <= 640)
    const path = join(root, `capture.${format}`)
    await writeFile(path, image.data)
    imagePaths.push(path)
    t.diagnostic(`${format}: ${image.width}x${image.height}, ${image.data.length} bytes`)
  }
  // Decode the returned bytes and reject blank captures, independently of the
  // SDK's MIME/dimension metadata and the fixture's accessibility content.
  const decoder = join(root, 'decode.swift')
  await writeFile(decoder, `
import AppKit
for path in CommandLine.arguments.dropFirst() {
    let data = try Data(contentsOf: URL(fileURLWithPath: path))
    guard let image = NSBitmapImageRep(data: data), image.pixelsWide > 0, image.pixelsHigh > 0 else {
        fatalError("Image cannot be decoded")
    }
    precondition(max(image.pixelsWide, image.pixelsHigh) <= 640, "Encoded image must respect maxDimension")
    var low = 1.0, high = 0.0
    for y in stride(from: 0, to: image.pixelsHigh, by: 3) {
        for x in stride(from: 0, to: image.pixelsWide, by: 3) {
            if let color = image.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) {
                let value = (color.redComponent + color.greenComponent + color.blueComponent) / 3
                low = min(low, value); high = max(high, value)
            }
        }
    }
    precondition(high - low > 0.5, "Capture must contain text and background, not a blank image")
}
`)
  execFileSync('swift', [decoder, ...imagePaths])

  const query = await computer.query({ appSessionId: session.id, windowId: target.windowId,
    role: 'AXTextArea', text: 'Document input', exact: true })
  assert.equal(query.snapshot.tree.status, 'available')
  assert.equal(query.snapshot.tree.elements.length, 1)
  const element = query.snapshot.tree.elements[0]
  await computer.perform({ type: 'selectText', appSessionId: session.id, snapshotId: query.snapshot.id,
    elementId: element.id, text: 'world' })
  await computer.perform({ type: 'typeText', appSessionId: session.id, snapshotId: query.snapshot.id,
    text: '跨Space🙂', keyMethod: 'skyKey' })
  await waitForState(state => state.view === 'hello 跨Space🙂')
  t.diagnostic('query/selectText/skyKey: native fixture readback matched hello 跨Space🙂')
  assert.equal((await computer.stopAppSession({ appSessionId: session.id })).cleanup, 'complete')
  await computer.close()
  assert.equal(fixture.exitCode, null, 'closing automation must leave the target app running')
})
