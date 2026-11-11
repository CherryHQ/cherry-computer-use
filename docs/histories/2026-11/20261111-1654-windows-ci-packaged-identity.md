## [2026-11-11 16:54] | Task: Make the Windows packaged identity test deterministic

### User Query

Fix the Windows CI failure on PR #35.

### Changes

- The Windows Server runner resolves Calculator to the legacy executable.
  Use a fixture AppsFolder entry to test packaged alias and AUMID resolution.
- Reuse the PowerShell test runner for the Shell target identity regression.
  The live icon test still queries the Calculator installed on the host.
