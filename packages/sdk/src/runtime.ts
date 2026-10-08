import { spawn } from 'node:child_process'
import { constants } from 'node:fs'
import { access, readFile, realpath, stat } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { dirname, join, resolve } from 'node:path'
import packageJson from '../package.json'
import { ComputerUseError } from './errors.js'

const require = createRequire(import.meta.url)

export async function resolveRuntimePath(
  runtimePath?: string
): Promise<string> {
  const { platform, arch } = process
  if (
    !['darwin', 'win32', 'linux'].includes(platform) ||
    !['arm64', 'x64'].includes(arch)
  ) {
    throw new ComputerUseError(
      'UNSUPPORTED_PLATFORM',
      `Unsupported runtime platform: ${platform}/${arch}`
    )
  }
  let path = runtimePath
  if (path === undefined) {
    const name = `@cherrystudio/computer-use-${platform}-${arch}`
    let manifestPath: string
    try {
      manifestPath = require.resolve(`${name}/package.json`)
    } catch (cause) {
      throw new ComputerUseError(
        'RUNTIME_NOT_FOUND',
        `Install ${name}@${packageJson.version} or provide runtimePath`,
        { cause }
      )
    }
    const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
    if (manifest.version !== packageJson.version) {
      throw new ComputerUseError(
        'RUNTIME_VERSION_MISMATCH',
        `${name} must match SDK version ${packageJson.version}`
      )
    }
    path = join(
      dirname(manifestPath),
      'runtime',
      platform === 'darwin'
        ? 'Open Computer Use.app'
        : platform === 'win32'
          ? 'open-computer-use.exe'
          : 'open-computer-use'
    )
  }
  if (typeof path !== 'string' || path.length === 0) {
    throw new ComputerUseError(
      'INVALID_ARGUMENT',
      'runtimePath must be a nonempty path'
    )
  }
  const executable =
    platform === 'darwin'
      ? join(resolve(path), 'Contents', 'MacOS', 'OpenComputerUse')
      : resolve(path)
  try {
    await access(
      executable,
      platform === 'win32' ? constants.F_OK : constants.X_OK
    )
    if (!(await stat(executable)).isFile())
      throw new Error('Runtime is not a file')
    // macOS must see the actual app bundle when starting through a dev symlink.
    return await realpath(executable)
  } catch (cause) {
    throw new ComputerUseError(
      'RUNTIME_NOT_FOUND',
      'The selected runtime executable is missing or not executable',
      { cause }
    )
  }
}

export function launchRuntime(executable: string, sessionId: string) {
  return spawn(executable, ['serve', '--stdio', '--session-id', sessionId], {
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
    shell: false
  })
}
