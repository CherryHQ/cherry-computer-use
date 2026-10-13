#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
failed=0

while IFS= read -r line; do
  file="${line%%:*}"
  content="${line#*:}"
  # 仓库内的本地 action / reusable workflow（uses: ./...）随当前提交走，无需固定 SHA
  if [[ "${content}" =~ uses:[[:space:]]*[\"\']?\./ ]]; then
    continue
  fi
  if [[ ! "${content}" =~ @[0-9a-f]{40}([[:space:]]|$) ]]; then
    echo "发现未固定 SHA 的 GitHub Action 引用: ${line}"
    failed=1
  fi
done < <(grep -RIn 'uses:' "${repo_root}/.github/workflows" || true)

if [[ "${failed}" -ne 0 ]]; then
  exit 1
fi

echo "GitHub Action 固定 SHA 检查通过"
