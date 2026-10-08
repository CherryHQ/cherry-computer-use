import { readFileSync, writeFileSync } from 'node:fs';
import { optionalDependencies, targets } from './sdk-distribution.mjs';

const root = new URL('../../', import.meta.url);
const sdkVersion = JSON.parse(readFileSync(new URL('packages/sdk/package.json', root))).version;
const version = JSON.parse(readFileSync(new URL('packages/cli/package.json', root))).version;
const check = process.argv.includes('--check');
function update(name, transform) {
  const file = new URL(name, root);
  const before = readFileSync(file, 'utf8');
  const after = transform(before);
  if (before === after) return;
  if (check) throw new Error(`${name} is out of sync with SDK ${sdkVersion} / CLI ${version}; run node scripts/npm/sync-versions.mjs`);
  writeFileSync(file, after);
}
update('plugins/open-computer-use/.codex-plugin/plugin.json', text => {
  const manifest = JSON.parse(text);
  if (manifest.version === version) return text;
  manifest.version = version;
  const updated = JSON.stringify(manifest, null, 2) + '\n';
  return text.includes('\r\n') ? updated.replaceAll('\n', '\r\n') : updated;
});
update('packages/OpenComputerUseKit/Sources/OpenComputerUseKit/OpenComputerUseVersion.swift', text =>
  text.replace(/(public let openComputerUseVersion = ")[^"]+(")/, `$1${sdkVersion}$2`));
for (const platform of ['Linux', 'Windows']) {
  update(`apps/OpenComputerUse${platform}/main.go`, text =>
    text.replace(/(var version = ")[^"]+(")/, `$1${sdkVersion}$2`));
}

update('packages/sdk/package.json', text => {
  const manifest = JSON.parse(text);
  const dependencies = optionalDependencies(sdkVersion);
  if (JSON.stringify(manifest.optionalDependencies) === JSON.stringify(dependencies)) return text;
  manifest.optionalDependencies = dependencies;
  const updated = JSON.stringify(manifest, null, 2) + '\n';
  return text.includes('\r\n') ? updated.replaceAll('\n', '\r\n') : updated;
});

for (const target of targets) {
  update(`packages/sdk-native/${target}/package.json`, text => {
    const manifest = JSON.parse(text);
    if (manifest.version === sdkVersion) return text;
    manifest.version = sdkVersion;
    const updated = JSON.stringify(manifest, null, 2) + '\n';
    return text.includes('\r\n') ? updated.replaceAll('\n', '\r\n') : updated;
  });
}
