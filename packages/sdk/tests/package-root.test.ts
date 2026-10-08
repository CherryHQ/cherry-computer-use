import assert from 'node:assert/strict'
import { posix, win32 } from 'node:path'
import { test } from 'node:test'
import { findPackageRoot } from '../scripts/package-root.js'

for (const [platform, paths, root] of [
  ['posix', posix, '/workspace'],
  ['win32', win32, 'C:\\workspace']
] as const) {
  for (const name of ['ajv', '@scope/package']) {
    test(`${platform} finds ${name} license root from a nested entry`, () => {
      const directory = paths.join(root, 'node_modules', name)
      assert.equal(findPackageRoot(paths.join(directory, 'dist', 'index.js'), name, paths), directory)
    })
  }
  test(`${platform} rejects missing package roots without looping at the filesystem root`, () => {
    assert.throws(() => findPackageRoot(paths.join(root, 'node_modules', 'ajv-extra', 'index.js'), 'ajv', paths), /Cannot find package root/)
  })
}
