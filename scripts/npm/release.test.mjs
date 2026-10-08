import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const root = fileURLToPath(new URL('../../', import.meta.url));
function fixture(t) {
  const dir = mkdtempSync(path.join(tmpdir(), 'cherry-release-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (const entry of ['scripts', 'packages/cli', 'packages/sdk/package.json', 'packages/sdk-native', 'plugins', '.agents/plugins', 'LICENSE', 'THIRD_PARTY_NOTICES.md']) {
    const target = path.join(dir, entry);
    mkdirSync(path.dirname(target), { recursive: true });
    cpSync(path.join(root, entry), target, {
      recursive: true,
      filter: name => !name.includes(`${path.sep}node_modules`) && !name.includes(`${path.sep}dist`),
    });
  }
  return dir;
}
function run(dir, ...args) {
  return spawnSync(process.execPath, args, { cwd: dir, encoding: 'utf8' });
}
function write(dir, name, text) {
  const file = path.join(dir, name);
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, text, { mode: 0o755 });
}

test('CLI staging refuses incomplete native artifacts and never creates upstream packages', t => {
  const dir = fixture(t);
  const args = ['scripts/npm/build-packages.mjs', '--skip-build'];
  assert.notEqual(run(dir, ...args).status, 0);
  for (const name of [
    'Cherry Computer Use.app/Contents/MacOS/OpenComputerUse',
    'linux/arm64/open-computer-use', 'linux/amd64/open-computer-use',
    'windows/arm64/open-computer-use.exe', 'windows/amd64/open-computer-use.exe',
  ]) write(dir, `dist/${name}`, 'native fixture');
  const built = run(dir, ...args);
  assert.equal(built.status, 0, built.stderr);
  const packageRoot = path.join(dir, 'dist/npm/computer-use-cli');
  const manifest = JSON.parse(readFileSync(path.join(packageRoot, 'package.json')));
  assert.equal(manifest.name, '@cherrystudio/computer-use-cli');
  assert.equal(manifest.private, true, 'local CLI tarballs must not become registry-publishable');
  assert.ok(existsSync(path.join(packageRoot, 'THIRD_PARTY_NOTICES.md')));
  for (const command of ['cherry-computer-use', 'open-computer-use', 'ocu']) {
    const help = run(dir, path.join(packageRoot, manifest.bin[command]), '--help');
    assert.equal(help.status, 0, help.stderr);
    assert.match(help.stdout, /Cherry Computer Use/);
  }
  const rejected = run(dir, ...args, '--package', 'open-computer-use');
  assert.notEqual(rejected.status, 0);
  assert.match(rejected.stderr, /Unsupported package name/);
});

test('SDK versions pin native packages and fallbacks while the CLI owns the plugin version', t => {
  const dir = fixture(t);
  const cliPath = path.join(dir, 'packages/cli/package.json');
  const cli = JSON.parse(readFileSync(cliPath));
  cli.version = '4.5.6';
  writeFileSync(cliPath, JSON.stringify(cli));
  const sdkPath = path.join(dir, 'packages/sdk/package.json');
  const sdk = JSON.parse(readFileSync(sdkPath));
  sdk.version = '1.2.3';
  writeFileSync(sdkPath, JSON.stringify(sdk));
  write(dir, 'packages/OpenComputerUseKit/Sources/OpenComputerUseKit/OpenComputerUseVersion.swift', 'public let openComputerUseVersion = "0.0.1"\n');
  for (const platform of ['Linux', 'Windows']) write(dir, `apps/OpenComputerUse${platform}/main.go`, 'var version = "0.0.1"\n');
  assert.notEqual(run(dir, 'scripts/npm/sync-versions.mjs', '--check').status, 0);
  assert.equal(run(dir, 'scripts/npm/sync-versions.mjs').status, 0);
  assert.equal(JSON.parse(readFileSync(path.join(dir, 'plugins/open-computer-use/.codex-plugin/plugin.json'))).version, '4.5.6');
  assert.match(readFileSync(path.join(dir, 'apps/OpenComputerUseLinux/main.go'), 'utf8'), /var version = "1\.2\.3"/);
  assert.match(readFileSync(path.join(dir, 'packages/OpenComputerUseKit/Sources/OpenComputerUseKit/OpenComputerUseVersion.swift'), 'utf8'), /Version = "1\.2\.3"/);
  assert.equal(run(dir, 'scripts/npm/sync-versions.mjs', '--check').status, 0);
  const dependencies = JSON.parse(readFileSync(sdkPath)).optionalDependencies;
  assert.equal(Object.keys(dependencies).length, 6);
  assert.ok(Object.values(dependencies).every(version => version === '1.2.3'));
  const pluginPath = path.join(dir, 'plugins/open-computer-use/.codex-plugin/plugin.json');
  const crlf = readFileSync(pluginPath, 'utf8').replace(/\r?\n/g, '\r\n');
  writeFileSync(pluginPath, crlf);
  const checked = run(dir, 'scripts/npm/sync-versions.mjs', '--check');
  assert.equal(checked.status, 0, checked.stderr);
  assert.equal(readFileSync(pluginPath, 'utf8'), crlf, 'check must preserve checkout line endings');
  writeFileSync(pluginPath, crlf.replace('4.5.6', '0.0.1'));
  assert.notEqual(run(dir, 'scripts/npm/sync-versions.mjs', '--check').status, 0);
  assert.equal(run(dir, 'scripts/npm/sync-versions.mjs').status, 0);
  assert.equal(readFileSync(pluginPath, 'utf8'), crlf, 'updates must preserve CRLF while fixing the version');
});

test('formal release cannot silently downgrade to ad-hoc when credentials are absent', () => {
  const result = spawnSync('bash', ['scripts/build-signed-release.sh'], {
    cwd: root, encoding: 'utf8', env: { ...process.env, CSC_LINK: '' },
  });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /CSC_LINK is required/);
});
