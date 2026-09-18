import { defineConfig } from 'tsup'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  platform: 'node',
  target: 'node24',
  dts: true,
  clean: true,
  shims: true,
  onSuccess: async () => {
    const { copyFile, readFile, writeFile } = await import('node:fs/promises')
    await copyFile('../../LICENSE', 'LICENSE')
    const require = createRequire(import.meta.url)
    const notices = await Promise.all(
      ['ajv', 'tsup'].map(async (name) => {
        const license = await readFile(
          join(dirname(require.resolve(`${name}/package.json`)), 'LICENSE'),
          'utf8'
        )
        return `${name}\n\n${license}`
      })
    )
    await writeFile('dist/THIRD_PARTY_NOTICES.txt', notices.join('\n'))
  }
})
