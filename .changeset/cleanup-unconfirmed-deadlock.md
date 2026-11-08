---
'@cherrystudio/computer-use': patch
---

Report a malformed response as a failed request instead of unconfirmed cleanup.

`RuntimeConnection.request` used to fail the whole connection when a result did not match the protocol.
Failing disposes both pipes, so the graceful shutdown could never be sent and `close()` always ended in
`CLEANUP_FAILED` with `cleanup: 'unconfirmed'`, which hosts surface as a permanent "cleanup unconfirmed"
state that blocks every later call. Response-shape defects now reject only that request, the session stays
usable, and shutdown can complete. Ownership checks moved to a separate `owned` predicate because a response
that claims another session's data is a trust breach, not a shape defect, and must still fail the connection.
Windows pre-dispatch refusals (UIA text fallback disabled, unsupported click methods, hosted scroll without
ScrollPattern) now report `effect: 'none'` with `UNSUPPORTED_CAPABILITY` instead of claiming a possible effect.
