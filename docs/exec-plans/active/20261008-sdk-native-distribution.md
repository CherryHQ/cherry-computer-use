# Deliver SDK native runtimes

## Goal and boundaries

Repair the incomplete 0.1.0 SDK distribution. Installing the SDK must install a matching native runtime and `ComputerUse.start()` must work without a development checkout or `runtimePath`. Keep the CLI private; do not publish from this development task.

## Implementation

- Generate six OS/CPU-specific npm packages, pinned exactly by the SDK's optional dependencies. One SDK version owns all runtime package versions; CLI/plugin versions remain independent.
- Build and install actual npm tarballs on all six native runner architectures. Serve candidate packages through an isolated local registry to exercise automatic optional dependency selection, ESM/CJS startup and confirmed shutdown.
- Require Developer ID signing, notarization, stapling and post-install verification for macOS release artifacts. Unsigned PR builds are explicitly separate from publishable artifacts.
- Create version PRs before requesting signing credentials. Build all packages after the version PR merges, gate publication on all six native jobs, publish native packages first, then the SDK. Preserve safe partial-release retries.
- Add a patch changeset, release safeguards, docs and history. Validate locally and on the final PR head; record Apple-service validation separately from unsigned CI.

## Progress

- Confirmed npm 0.1.0 contains only JS/types. Main includes both prior PRs and the 0.1.0 version PR; synchronized with a backup ref and verified the baseline tree matches origin/main.
- Implementation complete locally. SDK typecheck/build and 42 tests, release gate regressions and actionlint passed. macOS arm64 real tarball auto-install/start/close passed with ad-hoc signing. Remote six-architecture PR verification is next; Apple-service validation remains a release-time gate.

- Clean-checkout validation exposed npm lockfile resolution for unpublished optional dependencies. Added private development placeholders in a Changesets fixed group (private versioning enabled, tags disabled); public packages are still generated from real binaries. A version-PR simulation bumped SDK/all six dependencies to 0.1.1, retained private CLI 0.3.5 and passed npm ci.
- Seven release regressions, Swift 192 tests (one skipped), Linux/Windows Go unit tests and actionlint passed locally.
- The local 1Password signer previously blocked delivery. The user unlocked it on October 9; signed submission and remote verification are resuming. No registry writes have been performed; formal Apple-service validation remains a release-time gate.
