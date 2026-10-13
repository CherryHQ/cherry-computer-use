#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required_files=(
  ".gitignore"
  ".gitattributes"
  "CODEOWNERS"
  "CONTRIBUTING.md"
  "SECURITY.md"
  # 与 docs/CICD.md 描述的实际 workflow 保持一致
  ".github/workflows/sdk-check.yml"
  ".github/workflows/sdk-distribution.yml"
  ".github/workflows/release.yml"
  "scripts/check-action-pinning.sh"
)

failed=0

for path in "${required_files[@]}"; do
  if [[ ! -f "${repo_root}/${path}" ]]; then
    echo "缺少必要文件: ${path}"
    failed=1
  fi
done

if grep -q $'\r' "${repo_root}/README.md"; then
  echo "README.md 含有 CRLF 换行"
  failed=1
fi

if ! grep -q "make check-docs" "${repo_root}/CONTRIBUTING.md"; then
  echo "CONTRIBUTING.md 应明确提到 make check-docs"
  failed=1
fi

if [[ "${failed}" -ne 0 ]]; then
  exit 1
fi

echo "仓库基础卫生检查通过"
