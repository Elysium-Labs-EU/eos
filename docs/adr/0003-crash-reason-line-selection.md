# 0003. Crash-reason line selection: bound by pgid, prefer an error-shaped line

Date: 2026-08-09
Status: Accepted

## Context

`GetServiceLastErrorLine` picked the literal last non-empty stderr line as the
crash reason shown in `eos status`/health-monitor messages. Real Node.js and
Bun crash output routinely ends in a line with no diagnostic value: Node
prints a bare `Node.js vX.Y.Z` after its uncaught-exception handler; Bun
prints its version banner at the top of the *next* restart attempt, landing
in the same shared error log before the current attempt is even reported. The
useful line (`code: 'EADDRINUSE'`) sat several lines above, or nothing
surfaced at all.

## Decision

- Tag every stdout/stderr line the pipe-forwarding goroutines write with the
  launch's own pgid (process group leader's PID).
- Scan backward considering only lines tagged with the requested pgid, so a
  restarted attempt's own lines sharing the file can't be mistaken for the
  crash being reported.
- Within that pgid window, prefer the most recent line that looks
  error-shaped (contains `error`, `exception`, `errno`, `code:`, ...).
- No error-shaped line in the window: fall back to the window's own last
  non-empty line, so single-line synthetic crash scripts keep working.

## Rejected

- **Pure banner denylist**: grows one pattern per runtime eos supports, and a
  miss actively returns wrong data indistinguishable from a real reason. Also
  has no notion of "this restart's error" vs. "that restart's banner", so it
  can't fix cross-restart contamination on its own.
- **Error-shaped matching without pgid bounding**: fixes the trailing-banner
  case but not the cross-cycle one, since restarts share one error log file.
- **Capture the whole trailing block since the last error marker**: more
  robust, but breaks `GetServiceLastErrorLine`'s single-line contract and
  every call site embedding it inline. Deferred; the pgid bound already
  removes the failure mode block-capture was mainly reached for.

## Consequences

`GetServiceLastErrorLine` returns `ok=false` only when the pgid's window is
genuinely empty (log missing/unreadable, or nothing but breadcrumbs logged) —
`eos status` then shows no `: <reason>` suffix, a pre-existing degradation,
not a new one. The marker list is short by design: a miss only forfeits the
improvement, never regresses below the prior last-line behavior.
