import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import Ajv from 'ajv'
import standaloneCode from 'ajv/dist/standalone/index.js'
import { compile } from 'json-schema-to-typescript'

const schema = JSON.parse(
  await readFile(
    new URL('../../../protocol/schema.json', import.meta.url),
    'utf8'
  )
)
const output = new URL('../src/generated/', import.meta.url)
const banner = '// Generated from protocol/schema.json. Do not edit.\n'
await rm(output, { recursive: true, force: true })
await mkdir(output, { recursive: true })
await writeFile(
  new URL('protocol.ts', output),
  await compile(schema, 'Protocol', {
    bannerComment: banner.trim(),
    unreachableDefinitions: true,
    style: { semi: false, singleQuote: true }
  })
)

const names = [
  'InitializeInput',
  'InitializeResult',
  'ShutdownResult',
  'Capabilities',
  'PermissionStatus',
  'PermissionRequest',
  'AppList',
  'OpenAppSessionInput',
  'AppSessionInput',
  'AppSession',
  'AppSessionList',
  'StopAppSessionResult',
  'ObserveInput',
  'Snapshot',
  'Action',
  'ActionResult',
  'ErrorData',
  'RpcResponse'
]
const ajv = new Ajv({ code: { source: true }, strict: true })
ajv.addSchema(schema)
const validators = Object.fromEntries(
  names.map((name) => [`validate${name}`, `${schema.$id}#/definitions/${name}`])
)
await writeFile(
  new URL('validators.cjs', output),
  banner + standaloneCode(ajv, validators)
)
await writeFile(
  new URL('validators.d.cts', output),
  banner +
    names
      .map(
        (name) =>
          `export declare function validate${name}(value: unknown): value is import('./protocol.js').${name}`
      )
      .join('\n') +
    '\n'
)
