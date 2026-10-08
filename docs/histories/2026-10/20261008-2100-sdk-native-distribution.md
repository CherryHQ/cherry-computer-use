# Deliver SDK native platform packages

## Request

After SDK 0.1.0 was published without its native backend, implement full SDK runtime distribution while keeping the CLI private.

## Changes

- Generate six OS/CPU-filtered runtime packages; the SDK pins matching exact versions through optional dependencies. SDK versions now drive native binary and app metadata; CLI/plugin versions remain separate.
- Build one SDK tarball and install it against actual native tarballs using an isolated local registry on six native architectures. Verify automatic discovery, both ESM/CJS entry points, session startup and confirmed shutdown.
- Separate version PR preparation from signed native builds and publication. macOS release packages require Developer ID signing, notarization/stapling and post-install identity/signature checks.
- Validate all six artifact records, hashes and the identical tested SDK tarball before any npm writes. Publish platform packages first, then SDK; retry only same-source platform versions. CLI remains private.
- Remove accidental whitespace from the existing Cherry helper Bundle ID and related permission/notification constants, retaining its namespace.
- Add a patch changeset and update SDK, CI, architecture and release documentation. No registry publication or deprecation was performed during development.

## Validation

Local SDK typecheck/build and all 42 tests passed. Release regressions cover version synchronization, private CLI packaging, missing signing credentials, missing/unverified/unsigned artifacts, mismatched source/version and modified hashes. macOS arm64 installed the real ad-hoc helper from its platform tarball and passed ESM/CJS automatic startup/shutdown. Remote six-architecture CI passed; formal Apple-service validation remains separate. See the [completed plan](../../exec-plans/completed/20261008-sdk-native-distribution.md).

Clean-checkout and version-PR simulation also passed: private native placeholders resolve npm workspace dependencies before registry publication, and the fixed group moves SDK/all six packages to 0.1.1 while CLI stays 0.3.5. Seven release regressions, Swift 192 tests (one skipped), Linux/Windows Go unit tests and actionlint passed. The user unlocked 1Password on October 9; signed commits are published in PR #5 and all remote checks passed. Formal Apple-service validation has not run.

October 9 remote verification: the signed implementation is PR #5; existing three-platform checks and five native installation jobs passed. Intel macOS failed because its default Xcode provides Swift 6.1. The workflow now selects installed Xcode 26.2 before building the Swift 6.2 package.

Final implementation head `44f8e71` passed both three-platform sdk-check and all six native architecture installation/startup jobs. No npm release was performed.
