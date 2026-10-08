# SDK runtime development workspaces

These six private manifests let npm resolve the SDK's exact optional dependencies
inside a checkout before that version exists on the public registry. They contain
no native runtime and must never be published directly. Their versions follow
`packages/sdk/package.json` through `scripts/npm/sync-versions.mjs`; do not add
independent changesets for them. Changesets versions the seven-package fixed group together, while private package tagging and publication remain disabled.

`scripts/npm/build-sdk-runtime.mjs` generates the public OS/CPU-specific package
with its real binary in `dist/sdk-packages/<target>`. Clean-consumer CI installs
that tarball through an isolated registry, without workspace links. The guarded
release workflow publishes only verified generated tarballs, keeping these
placeholders and the CLI private.
