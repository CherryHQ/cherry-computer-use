## 2026-10-15 | Task: Authorize application identity before launch

### Execution Context
- Agent: Codex
- Runtime: local coding agent

### User request
Fix the stopped-app launch bypass found in Cherry Studio PR #21704, publish a patch SDK, and upgrade Cherry.

### Changes
- Add side-effect-free macOS installed identity resolution and carry stable bundle identity in app information.
- Verify the host-authorized identity before native launch; preserve running-only resolution.
- Keep installed lookup unsupported on Windows/Linux, whose SDK cannot launch apps.
- Add a native regression case for installed/symlink identity resolution and identity mismatch rejection before launch.

### Validation
- SDK TypeScript check and native Swift library build passed.
- No local automated tests run, per user instruction; regression tests are submitted for CI.
- Publishing and Cherry integration follow the existing Changesets pipeline.
