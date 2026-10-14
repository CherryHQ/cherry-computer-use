# Separate automation actions from observations

The user requested persistent Browser/Computer APIs without atomic MCP tools as an intermediate layer. The SDK now exposes additive `perform` and `resolveApp` methods through the typed client and native protocol.

`perform` skips post-action capture while consuming element/coordinate references. Keyboard input may retain its target, subject to native identity checks. macOS SDK text input sends keyboard events to the validated application target. `resolveApp` rejects ambiguous targets and supports background launch on macOS; Windows/Linux report unsupported launch explicitly.

Changed sources: protocol schema, SDK client/types/generator, Swift desktop/resolver, and the shared Go desktop/session code. A Changeset records the SDK/native release requirement. Existing `act` behavior remains available.

TypeScript, Swift and Go builds passed. Regression tests were added but not run locally at the user's request. Release integration and model-driven GUI acceptance remain tracked in [the execution plan](../../exec-plans/active/20261015-automation-observation-boundaries.md).

## 2026-10-15 main synchronization

Fast-forwarded the workspace to `origin/main` at `493c034` and restored the pending changes. Resolved the shared Go dispatcher conflict by retaining both upstream `requestPermissions` and local `resolveApp`; preserved the Linux implementation progress alongside the historical Cua Driver proposal.

Post-sync validation: shared `runtime-go` tests, SDK TypeScript check, documentation skeleton check and whitespace check passed. The pending work remains uncommitted.

## Observation and local integration follow-up

Added optional `includeScreenshot` to the protocol and all three native adapters; AX-only requests skip capture and cannot authorize coordinate input. Cherry selects this mode for AX reads. Explicit `launch: false` resolves without launch side effects. SDK/client regression sources cover skipped capture and coordinate denial. The local SDK and macOS Dev bundle build successfully and connect through the normal private SDK session. Windows x64/Linux ARM64 cross-compiles pass. No tests were run in this follow-up. macOS reports the distinct Dev app permissions as unknown, so GUI acceptance remains pending.

Cherry was restarted against the local SDK and matching Dev bundle. A real Claude Sonnet 4.6 Browser task completed with four REPL calls, one persistent tab, two action-plus-observation cells and one final image; no syntax/tool errors occurred. This is Browser integration evidence, not desktop or cross-platform GUI acceptance.

## Calculator acceptance failure and repair

After both permissions were granted, the model's calculator run failed because the SDK routed `typeText` through the legacy editable-field implementation. The rejected input was then classified as uncertain, closing the session. The SDK now sends text as keyboard events directly to the validated application PID, preserving session ownership, window freshness, host permissions and targeted delivery. It does not require an editable AX element and does not use AX value replacement. `setValue` retains explicit control-value semantics; the legacy MCP text-field path is unchanged.

The shared keyboard sender emits one complete grapheme per key event, rather than combining an entire expression in one event. Newlines send Return. Cherry's calculator example remains `typeText`; the API documentation now describes this distinction. The failed model run is preserved as a failure, not acceptance. The native build passes; a fresh model run remains pending system authorization.

Ad-hoc rebuilds invalidated the local permission identity. Dev builds now use the existing automatic Developer ID signing path. The stable signed identity requires a new OS grant; no signing key or machine path is recorded here.

## Real-model calculator acceptance and Cherry integration follow-up

With both Dev-app permissions granted, Claude Sonnet 4.6 completed the Calculator task in three REPL calls. It bound the app, cleared and entered `128*37+456=`, then entered `/8=`. Fresh AX state and delivered screenshots confirmed `5192 → 649`; no retries or tool errors occurred. This verifies the macOS targeted keyboard path in Cherry, not Windows/Linux GUI behavior or all stop/launch boundaries.

Cherry’s follow-up unifies bundled Browser/Computer skills and runtime API references, adds shared execution/recovery instructions and structured action/error records, and distinguishes unconfirmed native cleanup from user stop while preserving control blocking. SDK publication and the remaining acceptance checklist stay open; no SDK code was changed in this follow-up.

## Cold-start window readiness repair

The model-driven cold-start check exposed `cgWindowNotFound` immediately after background launch. `SDKAppResolver.swift` now preserves LaunchServices errors and waits, within the existing launch deadline, for a read-only window observation without screenshot capture or activation. Cancellation and window-unavailable failures preserve possible launch effects. A signed native build and a new real-model Calculator cold-start run passed: three cells, results `5192 → 649`, final screenshot. No local automated test suite was run.

The same Cherry model also completed Wikipedia search-box submission and final page screenshot in four cells without tool errors. A subsequent Calculator turn attempted input without observing first; the host rejected it with no effect, and the model recovered by rebinding before reaching 650. This verifies stale-input blocking and recovery, not universal model compliance or all Agent-runtime skill paths.

## Patch release CI follow-up

The first PR macOS CI build exposed a file-private error-conversion initializer called from the new application resolver. The converter is now module-internal, allowing both paths to preserve the same error mapping. Earlier local build evidence did not cover this final cross-file call; release readiness is determined by the corrected CI revision. No local test suite was run.
