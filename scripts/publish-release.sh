#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
: "${NPM_TOKEN:?NPM_TOKEN is required to publish Cherry Studio packages}"
export NODE_AUTH_TOKEN="${NODE_AUTH_TOKEN:-${NPM_TOKEN}}"
npm run release:versions:check
node scripts/npm/publish-sdk.mjs
