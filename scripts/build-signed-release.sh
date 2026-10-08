#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
for name in CSC_LINK CSC_KEY_PASSWORD APPLE_ID APPLE_APP_SPECIFIC_PASSWORD APPLE_TEAM_ID; do
  if [[ -z "${!name:-}" ]]; then
    echo "${name} is required for signed Cherry Computer Use releases" >&2
    exit 1
  fi
done
if [[ "$(uname -s)" != Darwin ]]; then
  echo "Signed releases require macOS" >&2
  exit 1
fi

signing_dir="$(mktemp -d "${TMPDIR:-/tmp}/cherry-signing.XXXXXX")"
keychain_path="${signing_dir}/release.keychain-db"
keychain_password="$(openssl rand -hex 24)"
cleanup() {
  security delete-keychain "${keychain_path}" >/dev/null 2>&1 || true
  rm -rf "${signing_dir}"
}
trap cleanup EXIT
export CHERRY_CERT_PATH="${signing_dir}/certificate.p12"
node --input-type=module <<'JS'
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const source = process.env.CSC_LINK;
let certificate;
if (source.startsWith('https://')) {
  const response = await fetch(source);
  if (!response.ok) throw new Error('Could not download CSC_LINK certificate');
  certificate = Buffer.from(await response.arrayBuffer());
} else if (source.startsWith('file://') || source.startsWith('/') || source.startsWith('./')) {
  certificate = await readFile(source.startsWith('file://') ? fileURLToPath(source) : source);
} else {
  certificate = Buffer.from(source, 'base64');
}
await writeFile(process.env.CHERRY_CERT_PATH, certificate, { mode: 0o600 });
JS
security create-keychain -p "${keychain_password}" "${keychain_path}" >/dev/null
security set-keychain-settings -lut 21600 "${keychain_path}"
security unlock-keychain -p "${keychain_password}" "${keychain_path}"
security import "${CHERRY_CERT_PATH}" -k "${keychain_path}" -P "${CSC_KEY_PASSWORD}" -T /usr/bin/codesign -T /usr/bin/security >/dev/null
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "${keychain_password}" "${keychain_path}" >/dev/null
identity="$(security find-identity -v -p codesigning "${keychain_path}" | sed -n 's/.*"\(Developer ID Application: .*\)"/\1/p' | head -n 1)"
if [[ -z "${identity}" || "${identity}" != *"(${APPLE_TEAM_ID})" ]]; then
  echo "CSC_LINK must contain a Developer ID Application certificate for APPLE_TEAM_ID" >&2
  exit 1
fi
export OPEN_COMPUTER_USE_CODESIGN_MODE=identity
export OPEN_COMPUTER_USE_CODESIGN_IDENTITY="${identity}"
export OPEN_COMPUTER_USE_CODESIGN_KEYCHAIN="${keychain_path}"
./scripts/build-open-computer-use-app.sh release --arch "${1:-native}"
app_path="${repo_root}/dist/Cherry Computer Use.app"
codesign --verify --deep --strict "${app_path}"
ditto -c -k --keepParent "${app_path}" "${signing_dir}/app.zip"
xcrun notarytool submit "${signing_dir}/app.zip" \
  --apple-id "${APPLE_ID}" --password "${APPLE_APP_SPECIFIC_PASSWORD}" \
  --team-id "${APPLE_TEAM_ID}" --wait --output-format json > "${signing_dir}/notary.json"
node -e 'if (JSON.parse(require("node:fs").readFileSync(process.argv[1])).status !== "Accepted") throw new Error("Apple notarization was not accepted")' "${signing_dir}/notary.json"
xcrun stapler staple "${app_path}"
xcrun stapler validate "${app_path}"
spctl --assess --type execute --verbose "${app_path}"
