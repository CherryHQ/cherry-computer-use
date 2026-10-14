# Automation observation boundaries

## Goal

Support host-owned persistent app handles without making every action capture a full screenshot/tree. The host retains permission, stop state and task ownership; native code retains target validation and cancellation.

## Scope and decisions

- Add `perform(Action)` returning a completed receipt scoped to the app session. Keep `act` unchanged.
- Consume element/coordinate snapshots on every action. Only `perform` keyboard input may reuse the last validated target; explicit observation or action failure invalidates it.
- Send macOS SDK text as application-targeted keyboard events, without requiring an editable AX field. Revalidate the saved focused window before keyboard dispatch.
- Add `getAppState({includeScreenshot: false})` to skip native capture while preserving AX and target validation. Coordinate actions require a screenshot-enabled observation.
- Add `resolveApp({query, launch})`. Reject ambiguous matches. macOS can launch an installed application without activation; Windows/Linux only resolve running applications and explicitly reject unsupported launching.
- Do not add an interpreter, model dependencies, global-input fallback or automatic action replay.
- Keep protocol version 2: new methods are additive and require matching SDK/native artifacts; existing methods remain compatible.

## Validation and progress

- [x] TypeScript wire schema/client and Swift/Go runtime changes.
- [x] TypeScript check, Swift library build and shared Go build.
- [x] Regression source for capture-free input, stale references and unsupported launch.
- [ ] CI executes targeted regression tests. User requested no local test suites.
- [x] Build the SDK and macOS Dev bundle; connect Cherry locally without changing its published dependency or lockfile.
- [ ] Publish matching SDK/native artifacts, then upgrade Cherry's pinned dependency.
- [x] Real model-driven Browser acceptance in Cherry: persistent tab, navigation/click with AX observations, final screenshot; four REPL cells, no tool errors.
- [x] Real model-driven macOS Calculator acceptance: three REPL cells, `typeText`, results `5192 → 649`, two delivered screenshots, no tool errors.
- [ ] Remaining Computer acceptance: user stop, revoked access, stale window and native cleanup failure.
- [x] Browser search-box entry/submission/result screenshot: four real-model cells, no tool errors.
- [x] Cross-turn Calculator recovery: unobserved input rejected without effect; fresh binding/observation then reached 650 without replay.
- [ ] Output-budget recovery and per-runtime skill-discovery acceptance.

## Risks and rollback

The linked checkout is synchronized with the 0.1.4 release source. New methods and observation options are still unreleased; matching SDK/native artifacts must ship together. No package version is bumped and no local checkout path is committed into Cherry. Retaining `act` allows existing consumers to continue using their previous contract. Build success does not establish macOS/Windows/Linux GUI behavior or signed distribution acceptance.

## Local integration follow-up

SDK TypeScript/build, macOS Dev app build, shared Go build, Linux ARM64 and Windows x64 cross-compiles passed. A native SDK session initialized, queried permissions and closed normally. The Dev application reports both macOS permissions as unknown; desktop acceptance requires granting this distinct development app. Regression sources were extended but not executed locally. Cross-compilation does not validate Windows PowerShell or Linux GUI behavior.

## Calculator acceptance follow-up

Both permissions initially became granted. The real model run exposed an SDK contract mismatch: `typeText` used the legacy editable-field implementation and rejected calculator input, which then poisoned the session as an uncertain action. SDK text now uses targeted keyboard events, one complete grapheme per event, with Return for newlines. `setValue` retains editable-control semantics. The documented calculator example remains valid; desktop acceptance is still open until rerun.

Ad-hoc rebuilds invalidated TCC identity. Dev builds now use the existing automatic Developer ID signing path; reauthorize the signed Dev application before retrying. The failed run does not establish success, launch behavior, stop handling or screenshot delivery.

## Cherry skill and error-contract follow-up

Cherry now routes Browser and Computer workflows through two bundled skills and shares each skill’s `references/api.md` with first-binding runtime output. Common REPL rules, explicit documentation recovery, structured action receipts and operation/target failures are wired separately. Builtin updates use the existing content-hash synchronization; capability filtering applies to Claude and Pi skill catalogs. Cleanup-unconfirmed is reported separately from user stop. This implementation still needs the remaining acceptance above and CI; no local test suite was run.

The signed Dev app now has both permissions. A fresh real-model calculator run passed. Earlier failure records remain historical evidence, not the current permission state. General tab management is outside the current-pane Browser implementation and remains unimplemented.

## Cold-launch readiness

A real-model cold-start run initially failed after LaunchServices returned but before Calculator had an accessible window. The SDK resolver now waits for a read-only, screenshot-free window observation after a new launch, honors cancellation/deadline, and reports launch callback errors. It never activates the application or replays input. A fresh cold-start run passed in three REPL cells with `5192 → 649` and a final screenshot. The signed native build passed; no local test suite ran.
