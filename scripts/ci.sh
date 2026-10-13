#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${repo_root}/scripts/check-docs.sh"
"${repo_root}/scripts/check-repo-hygiene.sh"
"${repo_root}/scripts/check-action-pinning.sh"

while IFS= read -r file; do
  bash -n "$file"
done < <(find "${repo_root}/scripts" -type f -name '*.sh' | sort)

while IFS= read -r file; do
  node --check "$file"
done < <(find "${repo_root}/scripts" -type f -name '*.mjs' | sort)

if command -v go >/dev/null 2>&1; then
  (
    cd "${repo_root}/apps/OpenComputerUseWindows"
    go test ./...
  )
  (
    cd "${repo_root}/apps/OpenComputerUseLinux"
    test -z "$(gofmt -l .)"
    go vet ./...
    go test ./...
  )
fi

if command -v cargo >/dev/null 2>&1; then
  (
    cd "${repo_root}/apps/OpenComputerUseLinux/wayland-helper"
    cargo fmt --check
    cargo clippy --locked --all-targets -- -D warnings
    cargo test --locked
    node "${repo_root}/scripts/wayland-helper-notices.mjs" --check
  )
fi

echo "基础 CI 检查通过"
