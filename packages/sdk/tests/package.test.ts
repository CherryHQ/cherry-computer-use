import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import {
  chmod,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile
} from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { promisify } from 'node:util'

const exec = promisify(execFile)
const sdk = fileURLToPath(new URL('..', import.meta.url))
const npmCli = process.env.npm_execpath
const tsc = fileURLToPath(
  new URL('../../../node_modules/typescript/bin/tsc', import.meta.url)
)

test(
  'tarball is self-contained for ESM/CJS and NodeNext types without source or native binaries',
  { timeout: 60_000 },
  async (t) => {
    assert.ok(npmCli, 'Run package tests through npm run sdk:test')
    const consumer = await mkdtemp(join(tmpdir(), 'computer-use-consumer-'))
    t.after(() => rm(consumer, { recursive: true, force: true }))
    const packed = await exec(
      process.execPath,
      [
        npmCli,
        'pack',
        '--ignore-scripts',
        '--json',
        '--pack-destination',
        consumer
      ],
      { cwd: sdk }
    )
    const [manifest] = JSON.parse(packed.stdout)
    const files: string[] = manifest.files.map(
      (file: { path: string }) => file.path
    )
    for (const path of [
      'dist/index.js',
      'dist/index.cjs',
      'dist/index.d.ts',
      'dist/index.d.cts',
      'README.md',
      'LICENSE'
    ]) {
      assert.ok(files.includes(path), `Tarball missing ${path}`)
    }
    assert.equal(
      files.some((path) =>
        /^(src|tests|scripts|node_modules|runtime)\//.test(path)
      ),
      false
    )
    await writeFile(
      join(consumer, 'package.json'),
      JSON.stringify({ private: true, type: 'module' })
    )
    await exec(
      process.execPath,
      [
        npmCli,
        'install',
        '--cache',
        join(consumer, 'npm-cache'),
        '--ignore-scripts',
        '--no-audit',
        '--no-fund',
        join(consumer, manifest.filename)
      ],
      { cwd: consumer }
    )
    const installed = join(consumer, 'node_modules/@cherrystudio/computer-use')
    assert.equal(
      JSON.parse(await readFile(join(installed, 'package.json'), 'utf8')).name,
      '@cherrystudio/computer-use'
    )

    for (const extension of ['mts', 'cts']) {
      await writeFile(
        join(consumer, `consumer.${extension}`),
        `
import { ComputerUse, ComputerUseError, type ComputerUseClient, type Action, type Snapshot } from '@cherrystudio/computer-use'
const pending: Promise<ComputerUseClient> = ComputerUse.start({}, { signal: new AbortController().signal })
async function use(client: ComputerUseClient, action: Action) {
  const session = await client.openAppSession({ appId: 'app' })
  const snapshot: Snapshot = await client.getAppState({ appSessionId: session.id })
  await client.stopAppSession({ appSessionId: session.id })
  if (snapshot.screenshot.status === 'available') {
    const data: Uint8Array = snapshot.screenshot.image.data
    void data
  }
  await client.act(action)
  await client.close()
}
// @ts-expect-error A click must select an element or provide both coordinates.
const invalid: Action = { type: 'click', appSessionId: 'control', snapshotId: 'snapshot' }
void [pending, use, invalid, ComputerUseError]
`
      )
    }
    await exec(
      process.execPath,
      [
        tsc,
        '--noEmit',
        '--strict',
        '--module',
        'NodeNext',
        '--target',
        'ES2024',
        'consumer.mts',
        'consumer.cts'
      ],
      { cwd: consumer }
    )

    const nativePackage = join(
      consumer,
      `node_modules/@cherrystudio/computer-use-${process.platform}-${process.arch}`
    )
    for (const format of ['esm', 'cjs']) {
      const entry = join(
        consumer,
        format === 'esm' ? 'consumer.mjs' : 'consumer.cjs'
      )
      const header =
        format === 'esm'
          ? "import { ComputerUse, ComputerUseError } from '@cherrystudio/computer-use'"
          : "const { ComputerUse, ComputerUseError } = require('@cherrystudio/computer-use')"
      await writeFile(
        entry,
        `${header}\n;(async () => {
      try { await ComputerUse.start() } catch (error) {
        if (!(error instanceof ComputerUseError)) throw error
        process.stdout.write(error.code)
        return
      }
      throw new Error('Expected missing runtime')
    })().catch(error => { process.stderr.write(String(error)); process.exitCode = 1 })\n`
      )
      assert.equal(
        (await exec(process.execPath, [entry], { cwd: consumer })).stdout,
        'RUNTIME_NOT_FOUND'
      )
    }
    await mkdir(nativePackage, { recursive: true })
    await writeFile(
      join(nativePackage, 'package.json'),
      JSON.stringify({ version: '0.0.0' })
    )
    assert.equal(
      (
        await exec(process.execPath, [join(consumer, 'consumer.mjs')], {
          cwd: consumer
        })
      ).stdout,
      'RUNTIME_VERSION_MISMATCH'
    )

    if (process.platform === 'win32') return
    await writeFile(
      join(nativePackage, 'package.json'),
      JSON.stringify({ version: manifest.version })
    )
    const executable =
      process.platform === 'darwin'
        ? join(
            nativePackage,
            'runtime/Open Computer Use.app/Contents/MacOS/OpenComputerUse'
          )
        : join(nativePackage, 'runtime/open-computer-use')
    await mkdir(dirname(executable), { recursive: true })
    const fixture = join(consumer, 'runtime.mjs')
    await copyFile(new URL('./fixtures/runtime.mjs', import.meta.url), fixture)
    await writeFile(
      executable,
      `#!${process.execPath}\nimport(${JSON.stringify(pathToFileURL(fixture).href)})\n`
    )
    await chmod(executable, 0o755)
    for (const extension of ['mjs', 'cjs']) {
      const header =
        extension === 'mjs'
          ? "import { ComputerUse } from '@cherrystudio/computer-use'"
          : "const { ComputerUse } = require('@cherrystudio/computer-use')"
      const entry = join(consumer, `runtime-consumer.${extension}`)
      await writeFile(
        entry,
        `${header}\n;(async () => {
      const client = await ComputerUse.start()
      try {
        const session = await client.openAppSession({ appId: 'app' })
        const state = await client.getAppState({ appSessionId: session.id })
        await client.stopAppSession({ appSessionId: session.id })
        if (state.screenshot.status !== 'available' || !(state.screenshot.image.data instanceof Uint8Array)) throw new Error('Missing decoded image')
        process.stdout.write(state.app.name)
      } finally { await client.close() }
    })().catch(error => { process.stderr.write(String(error)); process.exitCode = 1 })\n`
      )
      assert.equal(
        (await exec(process.execPath, [entry], { cwd: consumer })).stdout,
        '测试 🙂'
      )
    }
  }
)
