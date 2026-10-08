import assert from 'node:assert/strict';
import { execFile, execFileSync, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';
import { artifactDirectory, assertPlatformManifest, integrity, manifestFromTarball, optionalDependencies, packageName, sdkManifest, sourceCommit, tarballName } from './sdk-distribution.mjs';

const exec = promisify(execFile);
const sdk = sdkManifest();
const target = `${process.platform}-${process.arch}`;
const name = packageName(target);
const recordPath = path.join(artifactDirectory, `${target}.json`);
const record = JSON.parse(await readFile(recordPath));
const nativePath = path.join(artifactDirectory, tarballName(name, sdk.version));
const nativeBytes = await readFile(nativePath);
assert.equal(integrity(nativeBytes), record.integrity);
const manifest = manifestFromTarball(nativePath);
assertPlatformManifest(manifest, target, sdk.version, sourceCommit());
const sdkTarball = path.join(artifactDirectory, tarballName(sdk.name, sdk.version));
assert.deepEqual(manifestFromTarball(sdkTarball).optionalDependencies, optionalDependencies(sdk.version));

// An isolated registry tests the SDK's real optional dependency resolution, without
// publishing candidates or relying on a platform package already on npm.
const consumer = await mkdtemp(path.join(tmpdir(), 'cherry-sdk-install-'));
let registry;
const server = createServer((request, response) => {
  const pathname = decodeURIComponent(new URL(request.url, registry).pathname);
  if (pathname === '/runtime.tgz') {
    response.writeHead(200, { 'Content-Type': 'application/octet-stream' }).end(nativeBytes);
  } else if (pathname === `/${name}`) {
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      name, 'dist-tags': { latest: sdk.version }, versions: {
        [sdk.version]: { ...manifest, dist: { tarball: `${registry}/runtime.tgz`, integrity: integrity(nativeBytes) } },
      },
    }));
  } else response.writeHead(404).end('{}');
});
try {
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  registry = `http://127.0.0.1:${server.address().port}`;
  await writeFile(path.join(consumer, 'package.json'), JSON.stringify({ private: true }));
  await writeFile(path.join(consumer, 'user.npmrc'), '');
  await writeFile(path.join(consumer, 'global.npmrc'), '');
  await exec(process.execPath, [process.env.npm_execpath,
    'install', sdkTarball, '--ignore-scripts', '--no-audit', '--no-fund', '--registry', registry,
    '--cache', path.join(consumer, 'cache'),
  ], {
    cwd: consumer, timeout: 120000,
    env: { ...process.env, NPM_CONFIG_USERCONFIG: path.join(consumer, 'user.npmrc'), NPM_CONFIG_GLOBALCONFIG: path.join(consumer, 'global.npmrc') },
  });
  const installed = path.join(consumer, 'node_modules', name);
  assertPlatformManifest(JSON.parse(await readFile(path.join(installed, 'package.json'))), target, sdk.version, sourceCommit());
  if (process.platform === 'darwin') {
    const app = path.join(installed, 'runtime/Cherry Computer Use.app');
    execFileSync('codesign', ['--verify', '--deep', '--strict', app]);
    const identifier = execFileSync('/usr/libexec/PlistBuddy', ['-c', 'Print :CFBundleIdentifier', path.join(app, 'Contents/Info.plist')], { encoding: 'utf8' }).trim();
    assert.equal(identifier, 'com.cherryai.cherrystudio.computer-use');
    if (record.signed) {
      const signature = spawnSync('codesign', ['-dv', '--verbose=4', app], { encoding: 'utf8' });
      assert.equal(signature.status, 0, signature.stderr);
      assert.match(signature.stderr, /Authority=Developer ID Application:/);
      assert.ok(process.env.APPLE_TEAM_ID, 'APPLE_TEAM_ID is required to verify the release signer');
      assert.ok(signature.stderr.includes(`TeamIdentifier=${process.env.APPLE_TEAM_ID}\n`));
      execFileSync('xcrun', ['stapler', 'validate', app], { stdio: 'inherit' });
      execFileSync('spctl', ['--assess', '--type', 'execute', '--verbose', app], { stdio: 'inherit' });
    }
  }
  for (const extension of ['mjs', 'cjs']) {
    const header = extension === 'mjs'
      ? "import { ComputerUse } from '@cherrystudio/computer-use'"
      : "const { ComputerUse } = require('@cherrystudio/computer-use')";
    const entry = path.join(consumer, `smoke.${extension}`);
    await writeFile(entry, `${header}\n;(async () => {
      for (let attempt = 0; attempt < (process.platform === 'darwin' ? 10 : 1); attempt++) {
        const client = await ComputerUse.start()
        try {
          const capabilities = await client.getCapabilities()
          if (capabilities.platform !== process.platform) throw new Error('Wrong runtime platform')
          if ((await client.listAppSessions()).length !== 0) throw new Error('Unexpected app ownership')
          await client.getPermissionStatus()
        } finally { await client.close() }
      }
    })().catch(error => { console.error(error); process.exitCode = 1 })\n`);
    await exec(process.execPath, [entry], { cwd: consumer, timeout: 45000 });
  }
  record.verified = true;
  record.sdkIntegrity = integrity(await readFile(sdkTarball));
  await writeFile(recordPath, JSON.stringify(record, null, 2) + '\n');
  console.log(`${target}: clean install automatically selected the native package; ESM/CJS start and shutdown passed.`);
} finally {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await rm(consumer, { recursive: true, force: true });
}
