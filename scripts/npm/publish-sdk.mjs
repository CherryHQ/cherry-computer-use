import assert from 'node:assert/strict';
import { artifactDirectory, publishRelease, sdkManifest, sourceCommit, validateRelease } from './sdk-distribution.mjs';

assert.ok(process.argv.slice(2).every(arg => arg === '--check'), 'Only --check is supported');
const sdk = sdkManifest();
const source = sourceCommit();
const { candidates, sdkTarball, sdkIntegrity } = validateRelease(artifactDirectory, sdk, source);
console.log(`Validated SDK and all ${candidates.length} native packages for ${sdk.version}.`);
if (!process.argv.includes('--check')) {
  assert.equal(process.env.GITHUB_ACTIONS, 'true', 'Publish through the guarded main-branch release workflow');
  assert.equal(process.env.GITHUB_REPOSITORY, 'CherryHQ/cherry-computer-use');
  assert.equal(process.env.GITHUB_REF, 'refs/heads/main');
  assert.ok(process.env.NPM_TOKEN, 'NPM_TOKEN is required');
  await publishRelease({ candidates, sdk, sdkTarball, sdkIntegrity, source });
}
