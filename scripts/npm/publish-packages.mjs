#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

if (process.argv.length > 2) {
  throw new Error('Publishing now uses Changesets. Use npm run npm:pack for local tarballs, or npm run changeset:publish for a signed release.');
}
const result = spawnSync('bash', ['scripts/publish-release.sh'], {
  cwd: fileURLToPath(new URL('../../', import.meta.url)), stdio: 'inherit',
});
process.exit(result.status ?? 1);
