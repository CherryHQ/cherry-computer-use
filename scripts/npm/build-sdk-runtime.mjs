import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { artifactDirectory, integrity, npm, packageName, root, sdkManifest, sourceCommit, targets } from './sdk-distribution.mjs';

const target = `${process.platform}-${process.arch}`;
assert.ok(targets.includes(target), `Unsupported build host ${target}`);
assert.ok(process.argv.slice(2).every(arg => arg === '--signed'), 'Only --signed is supported');
const signed = process.argv.includes('--signed');
if (signed) assert.equal(process.platform, 'darwin', '--signed requires macOS');
const version = sdkManifest().version;
const run = (script, args = [], env = process.env) => execFileSync('bash', [script, ...args], { cwd: root, stdio: 'inherit', env });
let source;
let runtimeName;
if (process.platform === 'darwin') {
  if (signed) run('scripts/build-signed-release.sh', ['native']);
  else run('scripts/build-open-computer-use-app.sh', ['release', '--arch', 'native'], { ...process.env, OPEN_COMPUTER_USE_CODESIGN_MODE: 'adhoc' });
  runtimeName = 'Cherry Computer Use.app';
  source = path.join(root, 'dist', runtimeName);
} else {
  const platform = process.platform === 'win32' ? 'windows' : 'linux';
  const arch = process.arch === 'x64' ? 'amd64' : 'arm64';
  run(`scripts/build-open-computer-use-${platform}.sh`, ['--arch', arch]);
  runtimeName = `open-computer-use${process.platform === 'win32' ? '.exe' : ''}`;
  source = path.join(root, 'dist', platform, arch, runtimeName);
}

const directory = path.join(root, 'dist/sdk-packages', target);
rmSync(directory, { recursive: true, force: true });
mkdirSync(path.join(directory, 'runtime'), { recursive: true });
cpSync(source, path.join(directory, 'runtime', runtimeName), { recursive: true, verbatimSymlinks: true });
for (const file of ['LICENSE', 'THIRD_PARTY_NOTICES.md']) cpSync(path.join(root, file), path.join(directory, file));
const manifest = {
  name: packageName(target), version, description: `Cherry Computer Use native runtime for ${target}`,
  license: 'MIT', repository: sdkManifest().repository, os: [process.platform], cpu: [process.arch],
  files: ['runtime', 'LICENSE', 'THIRD_PARTY_NOTICES.md'], publishConfig: { access: 'public' },
  cherryComputerUse: { source: sourceCommit(), signed },
};
writeFileSync(path.join(directory, 'package.json'), JSON.stringify(manifest, null, 2) + '\n');
mkdirSync(artifactDirectory, { recursive: true });
const [packed] = JSON.parse(npm(['pack', '--ignore-scripts', '--json', '--pack-destination', artifactDirectory], { cwd: directory }));
writeFileSync(path.join(artifactDirectory, `${target}.json`), JSON.stringify({
  target, version, source: sourceCommit(), signed, verified: false, filename: packed.filename,
  integrity: integrity(readFileSync(path.join(artifactDirectory, packed.filename))),
}, null, 2) + '\n');
console.log(`Packed ${manifest.name}@${version}; installation verification is required before publishing.`);
