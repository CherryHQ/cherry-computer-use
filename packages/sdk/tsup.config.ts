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
  // Bundle runtime deps so dist resolves nothing from node_modules; a `link:` consumer's
  // packager only sees its own dependency graph and would drop them.
  noExternal: ['ajv', 'json-rpc-2.0', 'vscode-jsonrpc'],
  // Inlined CommonJS still calls require() at runtime; give the ESM bundle one.
  banner: ({ format }) =>
    format === 'esm'
      ? { js: "import { createRequire as __createRequire } from 'node:module'; const require = __createRequire(import.meta.url);" }
      : {},
  onSuccess: async () => {
    const { copyFile, readFile, writeFile } = await import('node:fs/promises')
    await copyFile('../../LICENSE', 'LICENSE')
    const require = createRequire(import.meta.url)
    // Walk up from the resolved entry: exports maps (vscode-jsonrpc) hide package.json.
    const packageRoot = (name: string) => {
      let dir = dirname(require.resolve(name))
      while (!dir.endsWith(`/node_modules/${name}`)) dir = dirname(dir)
      return dir
    }
    const licenses = [
      ['ajv', 'LICENSE'],
      ['json-rpc-2.0', 'LICENSE'],
      ['tsup', 'LICENSE'],
      ['vscode-jsonrpc', 'License.txt']
    ] as const
    const notices = await Promise.all(
      licenses.map(async ([name, file]) => {
        const license = await readFile(join(packageRoot(name), file), 'utf8')
        return `${name}\n\n${license}`
      })
    )
    await writeFile('dist/THIRD_PARTY_NOTICES.txt', notices.join('\n'))
  }
})
