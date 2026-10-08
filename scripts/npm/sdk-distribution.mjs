import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const root = fileURLToPath(new URL('../../', import.meta.url));
export const targets = ['darwin-arm64', 'darwin-x64', 'win32-arm64', 'win32-x64', 'linux-arm64', 'linux-x64'];
export const sdkManifest = () => JSON.parse(readFileSync(path.join(root, 'packages/sdk/package.json')));
export const packageName = target => `@cherrystudio/computer-use-${target}`;
export const optionalDependencies = version => Object.fromEntries(targets.map(target => [packageName(target), version]));
export const tarballName = (name, version) => `${name.replace('@', '').replace('/', '-')}-${version}.tgz`;
export const integrity = data => `sha512-${createHash('sha512').update(data).digest('base64')}`;
export const sourceCommit = () => execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
export const artifactDirectory = path.join(root, 'dist/sdk-artifacts');

export function npm(args, options = {}) {
  assert.ok(process.env.npm_execpath, 'Run this command through its npm script');
  return execFileSync(process.execPath, [process.env.npm_execpath, ...args], {
    cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], ...options,
  });
}

export function manifestFromTarball(filename) {
  // A relative archive name avoids GNU tar interpreting a Windows drive colon
  // as a remote host. Both bsdtar and GNU tar support this form.
  return JSON.parse(execFileSync('tar', ['-xOf', path.basename(filename), 'package/package.json'], {
    cwd: path.dirname(filename), encoding: 'utf8',
  }));
}

export function assertPlatformManifest(manifest, target, version, source) {
  const [os, cpu] = target.split('-');
  assert.equal(manifest.name, packageName(target));
  assert.equal(manifest.version, version);
  assert.deepEqual(manifest.os, [os]);
  assert.deepEqual(manifest.cpu, [cpu]);
  assert.equal(manifest.private, undefined);
  assert.equal(manifest.bin, undefined, 'SDK runtime packages do not expose a CLI');
  assert.equal(manifest.scripts, undefined, 'Runtime packages must not run installation scripts');
  assert.equal(manifest.cherryComputerUse?.source, source);
}

export async function registryManifest(name, version) {
  const response = await fetch(`https://registry.npmjs.org/${encodeURIComponent(name)}/${encodeURIComponent(version)}`, {
    cache: 'no-store', signal: AbortSignal.timeout(30000),
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`Registry lookup failed for ${name}@${version}: HTTP ${response.status}`);
  return response.json();
}

export function validateRelease(directory, sdk, source) {
  // Validate the entire release before the first registry write. A green SDK build
  // alone must never allow publishing without all six tested native packages.
  const sdkTarball = path.join(directory, tarballName(sdk.name, sdk.version));
  const sdkIntegrity = integrity(readFileSync(sdkTarball));
  const packedSDK = manifestFromTarball(sdkTarball);
  assert.equal(packedSDK.name, sdk.name);
  assert.equal(packedSDK.version, sdk.version);
  assert.deepEqual(packedSDK.optionalDependencies, optionalDependencies(sdk.version));
  const candidates = targets.map(target => {
    const filename = path.join(directory, tarballName(packageName(target), sdk.version));
    const record = JSON.parse(readFileSync(path.join(directory, `${target}.json`)));
    assert.equal(record.target, target);
    assert.equal(record.version, sdk.version);
    assert.equal(record.source, source);
    assert.equal(record.integrity, integrity(readFileSync(filename)));
    assert.equal(record.sdkIntegrity, sdkIntegrity, 'Every platform must test the exact SDK tarball being published');
    assert.equal(record.verified, true, `${target} has not passed installation and startup verification`);
    const manifest = manifestFromTarball(filename);
    assertPlatformManifest(manifest, target, sdk.version, source);
    assert.equal(manifest.cherryComputerUse.signed, record.signed);
    if (target.startsWith('darwin-')) assert.equal(record.signed, true, 'Unsigned macOS artifacts cannot be published');
    return { filename, manifest, target };
  });
  assert.equal(JSON.parse(readFileSync(new URL('../../packages/cli/package.json', import.meta.url))).private, true);
  return { candidates, sdkTarball, sdkIntegrity };
}

export async function publishRelease({ candidates, sdk, sdkTarball, sdkIntegrity, source }, {
  lookup = registryManifest,
  publish = filename => npm(['publish', filename, '--ignore-scripts', '--access', 'public', '--provenance'], { stdio: 'inherit' }),
  wait = () => new Promise(resolve => setTimeout(resolve, 5000)),
} = {}) {
  // Detect occupied SDK/native versions before the first registry write too.
  const [existingSDK, ...existingNative] = await Promise.all([
    lookup(sdk.name, sdk.version), ...candidates.map(({ manifest }) => lookup(manifest.name, sdk.version)),
  ]);
  const checkExisting = (existing, { target, manifest }) => {
    assertPlatformManifest(existing, target, sdk.version, source);
    assert.equal(existing.cherryComputerUse.signed, manifest.cherryComputerUse.signed);
  };
  existingNative.forEach((existing, index) => { if (existing) checkExisting(existing, candidates[index]); });
  if (existingSDK) {
    assert.equal(existingSDK.dist.integrity, sdkIntegrity, 'SDK version is already occupied by different contents');
    assert.ok(existingNative.every(Boolean), 'Published SDK is missing native dependencies');
    return;
  }
  for (const [index, candidate] of candidates.entries()) {
    if (!existingNative[index]) await publish(candidate.filename);
  }
  for (const candidate of candidates) {
    let visible;
    for (let attempt = 0; attempt < 12; attempt++) {
      visible = await lookup(candidate.manifest.name, sdk.version);
      if (visible) break;
      await wait();
    }
    assert.ok(visible, `${candidate.manifest.name} is not yet visible; rerun the release before publishing the SDK`);
    checkExisting(visible, candidate);
  }
  await publish(sdkTarball);
}
