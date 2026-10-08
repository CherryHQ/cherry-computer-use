import { spawnSync } from 'node:child_process';
import { cpSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const result = spawnSync(process.execPath, ['scripts/npm/build-packages.mjs', '--skip-build'], {
  cwd: root, stdio: 'inherit',
});
if (result.status !== 0) process.exit(result.status ?? 1);
for (const entry of ['bin', 'dist', 'scripts', 'plugins', '.agents', 'LICENSE', 'THIRD_PARTY_NOTICES.md']) {
  const destination = path.join(root, 'packages/cli', entry);
  rmSync(destination, { recursive: true, force: true });
  cpSync(path.join(root, 'dist/npm/computer-use-cli', entry), destination, { recursive: true });
}
