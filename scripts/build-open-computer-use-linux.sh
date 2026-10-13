#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
arch="arm64"
configuration="release"
out_dir="${repo_root}/dist/linux"
wayland_helper="auto"
# Oldest glibc the Wayland helper may require; release builds hold this floor.
glibc_floor="2.28"

print_help() {
  cat <<'EOF'
Usage:
  scripts/build-open-computer-use-linux.sh [options]

Options:
  --arch arm64|amd64       Linux target architecture. Defaults to arm64.
  --configuration release  Reserved for parity with the macOS build script.
  --out-dir <dir>          Output directory. Defaults to dist/linux.
  --portable-helper        Require the Wayland helper, built with cargo-zigbuild
                           against glibc 2.28 (release builds).
  --no-wayland-helper      Build only the Go runtime.
  -h, --help               Show help.

The Wayland helper (open-computer-use-wayland) is placed next to the runtime.
Without --portable-helper it is built with cargo-zigbuild when available, else
with plain cargo for the host architecture (then it needs the host's glibc), and
skipped when neither works.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --arch)
      arch="${2:?--arch requires a value}"
      shift 2
      ;;
    --configuration)
      configuration="${2:?--configuration requires a value}"
      shift 2
      ;;
    --out-dir)
      out_dir="$(cd "${repo_root}" && mkdir -p "$2" && cd "$2" && pwd)"
      shift 2
      ;;
    --portable-helper)
      wayland_helper="portable"
      shift
      ;;
    --no-wayland-helper)
      wayland_helper="none"
      shift
      ;;
    -h|--help)
      print_help
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      print_help >&2
      exit 1
      ;;
  esac
done

case "${arch}" in
  arm64|amd64) ;;
  *)
    echo "Unsupported Linux arch: ${arch}" >&2
    exit 1
    ;;
esac

version="$(node -e "console.log(require('./packages/sdk/package.json').version)")"
module_dir="${repo_root}/apps/OpenComputerUseLinux"
output_dir="${out_dir}/${arch}"
mkdir -p "${output_dir}"

(
  cd "${module_dir}"
  GOOS=linux GOARCH="${arch}" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${version}" \
    -o "${output_dir}/open-computer-use" \
    .
)

echo "Built ${configuration} Linux runtime: ${output_dir}/open-computer-use"

helper_dir="${module_dir}/wayland-helper"
helper_path="${output_dir}/open-computer-use-wayland"
rm -f "${helper_path}"
[[ "${wayland_helper}" == "none" ]] && exit 0

case "${arch}" in
  amd64) rust_target="x86_64-unknown-linux-gnu" ;;
  arm64) rust_target="aarch64-unknown-linux-gnu" ;;
esac
host_arch="$(uname -m)"
skip_helper() {
  if [[ "${wayland_helper}" == "portable" ]]; then
    echo "$1" >&2
    exit 1
  fi
  echo "Warning: $1; the runtime reports Wayland input and capture as missing." >&2
  exit 0
}

if ! command -v cargo >/dev/null 2>&1; then
  skip_helper "cargo is not installed, so the Wayland helper was not built"
fi
# cargo-zigbuild finds zig on PATH or in the ziglang Python package.
if cargo zigbuild --help >/dev/null 2>&1 && { command -v zig >/dev/null 2>&1 || python3 -c 'import ziglang' >/dev/null 2>&1; }; then
  # zig links against the glibc floor's symbol versions, so one build runs on
  # every distribution from that glibc on, and can cross-compile either arch.
  rustup target add "${rust_target}" >/dev/null 2>&1 || true
  (cd "${helper_dir}" && cargo zigbuild --locked --release --target "${rust_target}.${glibc_floor}")
elif [[ "${wayland_helper}" != "portable" && "$(uname -s)" == "Linux" && "${rust_target}" == "${host_arch}-unknown-linux-gnu" ]]; then
  echo "Warning: cargo-zigbuild or zig is missing; the Wayland helper needs this host's glibc or newer." >&2
  (cd "${helper_dir}" && cargo build --locked --release --target "${rust_target}")
else
  skip_helper "cargo-zigbuild and zig are required to build the Wayland helper for ${arch}"
fi
cp "${helper_dir}/target/${rust_target}/release/open-computer-use-wayland" "${helper_path}"
chmod 755 "${helper_path}"

# The newest glibc symbol version the helper links against.
required="$(LC_ALL=C grep -aoE 'GLIBC_2\.[0-9]+' "${helper_path}" | awk -F. '$2 > max { max = $2 } END { print "2." max }')"
floor_minor="${glibc_floor#2.}"
if [[ "${wayland_helper}" == "portable" && "${required#2.}" -gt "${floor_minor}" ]]; then
  echo "The Wayland helper requires glibc ${required}, above the ${glibc_floor} floor." >&2
  exit 1
fi
echo "Built Wayland helper: ${helper_path} (requires glibc ${required})"
