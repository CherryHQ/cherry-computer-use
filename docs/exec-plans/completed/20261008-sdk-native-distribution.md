# SDK native runtime distribution — completed

## Delivered

PR #5 repairs the client-only 0.1.0 distribution with six exactly versioned native packages and automatic SDK runtime discovery. The CLI remains private. A patch changeset prepares 0.1.1; no package was published during implementation.

- Six private development placeholders keep clean npm installs working before registry publication. Changesets versions them in a fixed group with the SDK; only generated platform tarballs containing real binaries are publishable.
- Every native runner installs the same SDK tarball through an isolated registry and verifies automatic platform selection, ESM/CJS startup and confirmed shutdown.
- Formal macOS builds require Developer ID signing, notarization, stapling and installed-bundle verification. PR builds use ad-hoc signatures and cannot pass the formal publish gate.
- The release gate checks all six candidates, versions, source commits, hashes and verification records before registry writes. Native packages become visible before the SDK is published; partial retries require matching source identities.
- Removed accidental whitespace from the existing helper Bundle ID. Intel macOS explicitly selects Xcode 26.2 because the default toolchain did not satisfy Swift 6.2.

## Validation

- Local SDK typecheck/build and 42 tests passed; seven release regressions passed, including fixed-group versioning, occupied versions, partial publication and artifact rejection.
- Clean npm ci and an isolated version-PR simulation passed: SDK/all six runtime packages become 0.1.1, CLI remains private at 0.3.5.
- Swift: 192 tests, one skipped, no failures. Windows/Linux Go unit tests and actionlint passed.
- [Three-platform sdk-check](https://github.com/CherryHQ/cherry-computer-use/actions/runs/37851301684) passed on implementation head `44f8e71`.
- [SDK tarball and all six native architecture jobs](https://github.com/CherryHQ/cherry-computer-use/actions/runs/37851301705) passed on the same head, including real tarball installation and startup.
- The local 1Password signing block was resolved; implementation commits are signed and pushed in [PR #5](https://github.com/CherryHQ/cherry-computer-use/pull/5).

## Release-time boundaries

Formal Apple-service signing/notarization and npm publication have not been executed by this task. They remain mandatory release gates after the version PR merges. macOS GUI permissions, Wayland and packaged Electron delivery remain separate acceptance work; lifecycle tests do not establish those behaviors.
