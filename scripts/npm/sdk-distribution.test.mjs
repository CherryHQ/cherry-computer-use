import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { integrity, optionalDependencies, packageName, publishRelease, root, targets, tarballName, validateRelease } from './sdk-distribution.mjs';

test('release gate rejects incomplete, untested, unsigned and mismatched candidate sets before publishing', t => {
  const directory = mkdtempSync(path.join(tmpdir(), 'sdk-release-gate-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const staging = path.join(directory, 'stage');
  mkdirSync(path.join(staging, 'package'), { recursive: true });
  const source = 'test-source-commit';
  const sdk = { name: '@cherrystudio/computer-use', version: '1.2.3' };
  function pack(manifest) {
    writeFileSync(path.join(staging, 'package/package.json'), JSON.stringify(manifest));
    const filename = tarballName(manifest.name, manifest.version);
    execFileSync('tar', ['-czf', filename, '-C', 'stage', 'package'], { cwd: directory });
    return { filename, integrity: integrity(readFileSync(path.join(directory, filename))) };
  }
  const client = pack({ ...sdk, optionalDependencies: optionalDependencies(sdk.version) });
  assert.throws(() => validateRelease(directory, sdk, source), /ENOENT/);
  for (const target of targets) {
    const [os, cpu] = target.split('-');
    const native = pack({ name: packageName(target), version: sdk.version, os: [os], cpu: [cpu], cherryComputerUse: { source, signed: os === 'darwin' } });
    writeFileSync(path.join(directory, `${target}.json`), JSON.stringify({
      ...native, target, version: sdk.version, source, signed: os === 'darwin', verified: true, sdkIntegrity: client.integrity,
    }));
  }
  assert.equal(validateRelease(directory, sdk, source).candidates.length, 6);
  const recordPath = path.join(directory, 'darwin-arm64.json');
  const record = JSON.parse(readFileSync(recordPath));
  for (const change of [
    { verified: false }, { signed: false }, { source: 'another-commit' },
    { version: '1.2.4' }, { integrity: 'tampered' }, { sdkIntegrity: 'different-sdk' },
  ]) {
    writeFileSync(recordPath, JSON.stringify({ ...record, ...change }));
    assert.throws(() => validateRelease(directory, sdk, source), `Must reject ${JSON.stringify(change)}`);
  }
  // Both the manifest and its record can describe an ad-hoc PR build; it still
  // must never pass the formal release gate.
  const unsigned = pack({ name: packageName('darwin-arm64'), version: sdk.version, os: ['darwin'], cpu: ['arm64'], cherryComputerUse: { source, signed: false } });
  writeFileSync(recordPath, JSON.stringify({ ...record, ...unsigned, signed: false }));
  assert.throws(() => validateRelease(directory, sdk, source), /Unsigned macOS/);
});

test('publication preflights all versions, resumes partial native releases and publishes SDK last', async () => {
  const sdk = { name: '@cherrystudio/computer-use', version: '1.2.3' };
  const source = 'release-source';
  const candidates = targets.map(target => ({ target, filename: `${target}.tgz`, manifest: {
    name: packageName(target), version: sdk.version, os: [target.split('-')[0]], cpu: [target.split('-')[1]],
    cherryComputerUse: { source, signed: target.startsWith('darwin-') },
  } }));
  const release = { sdk, source, candidates, sdkTarball: 'sdk.tgz', sdkIntegrity: 'sdk-integrity' };
  const registry = new Map([[candidates[0].manifest.name, candidates[0].manifest]]);
  const writes = [];
  const adapters = {
    lookup: async name => registry.get(name) ?? null,
    publish: async filename => {
      writes.push(filename);
      const candidate = candidates.find(item => item.filename === filename);
      if (candidate) registry.set(candidate.manifest.name, candidate.manifest);
    },
    wait: async () => {},
  };
  registry.set(sdk.name, { dist: { integrity: 'old-client-only-sdk' } });
  await assert.rejects(publishRelease(release, adapters), /already occupied/);
  assert.deepEqual(writes, []);
  registry.delete(sdk.name);
  registry.set(candidates[5].manifest.name, { ...candidates[5].manifest, cherryComputerUse: { source: 'wrong-source' } });
  await assert.rejects(publishRelease(release, adapters));
  assert.deepEqual(writes, []);
  registry.delete(candidates[5].manifest.name);
  await publishRelease(release, adapters);
  assert.deepEqual(writes, [...candidates.slice(1).map(item => item.filename), 'sdk.tgz']);
});

test('native publication failure or registry invisibility prevents publishing the SDK', async () => {
  const sdk = { name: '@cherrystudio/computer-use', version: '1.2.3' };
  const release = { sdk, source: 'source', sdkTarball: 'sdk.tgz', sdkIntegrity: 'hash', candidates: [
    { target: 'linux-x64', filename: 'native.tgz', manifest: { name: packageName('linux-x64') } },
  ] };
  const writes = [];
  await assert.rejects(publishRelease(release, {
    lookup: async () => null,
    publish: async filename => { writes.push(filename); throw new Error('native publish failed'); },
  }), /native publish failed/);
  assert.deepEqual(writes, ['native.tgz']);
  writes.length = 0;
  await assert.rejects(publishRelease(release, {
    lookup: async () => null, publish: async filename => writes.push(filename), wait: async () => {},
  }), /not yet visible/);
  assert.deepEqual(writes, ['native.tgz']);
});

test('Changesets bumps the SDK and private native placeholders together while keeping CLI unchanged', t => {
  const directory = mkdtempSync(path.join(tmpdir(), 'sdk-version-plan-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  function write(name, data) {
    const filename = path.join(directory, name);
    mkdirSync(path.dirname(filename), { recursive: true });
    writeFileSync(filename, JSON.stringify(data));
  }
  write('package.json', JSON.parse(readFileSync(path.join(root, 'package.json'))));
  write('package-lock.json', JSON.parse(readFileSync(path.join(root, 'package-lock.json'))));
  const config = JSON.parse(readFileSync(path.join(root, '.changeset/config.json')));
  write('.changeset/config.json', { ...config, changelog: false });
  write('packages/sdk/package.json', { name: '@cherrystudio/computer-use', version: '7.0.0', optionalDependencies: optionalDependencies('7.0.0') });
  write('packages/cli/package.json', { name: '@cherrystudio/computer-use-cli', version: '0.3.5', private: true });
  for (const target of targets) write(`packages/sdk-native/${target}/package.json`, { name: packageName(target), version: '7.0.0', private: true });
  writeFileSync(path.join(directory, '.changeset/fix.md'), '---\n"@cherrystudio/computer-use": patch\n---\n\nRepair runtime distribution.\n');
  execFileSync(process.execPath, [path.join(root, 'node_modules/@changesets/cli/bin.js'), 'version'], { cwd: directory, stdio: 'inherit' });
  const sdk = JSON.parse(readFileSync(path.join(directory, 'packages/sdk/package.json')));
  assert.equal(sdk.version, '7.0.1');
  assert.deepEqual(sdk.optionalDependencies, optionalDependencies('7.0.1'));
  for (const target of targets) {
    const manifest = JSON.parse(readFileSync(path.join(directory, `packages/sdk-native/${target}/package.json`)));
    assert.equal(manifest.version, sdk.version);
    assert.equal(manifest.private, true);
  }
  assert.equal(JSON.parse(readFileSync(path.join(directory, 'packages/cli/package.json'))).version, '0.3.5');
});
