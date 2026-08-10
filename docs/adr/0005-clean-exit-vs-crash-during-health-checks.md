# 0005. Distinguish a clean exit-0 from a crash during health checks

Date: 2026-08-10
Status: Accepted

## Context

`checkStartProcess`/`checkRunningProcess` treated any process death as a
crash: liveness alone (`isProcessAlive`) decided, with no look at how the
process actually exited. eos has no `type: oneshot` (or similar) service
concept, so a `service.yaml` whose `command` is a legitimate one-shot build
step (no server, no `port:`) finishes, exits 0, and gets logged as "died
during startup"/"is not running", marked Failed, and restarted by the
backoff loop — rerunning the whole build every cycle until the restart
counter settles.

## Decision

- Capture the real exit code where it is actually available: the reaper
  goroutine already blocking on `cmd.Wait()` after every launch
  (`captureIdentity`), keyed by PGID so a caller holding only the PGID (the
  health monitor never sees the `exec.Cmd`) can read it back once.
- The read is consume-once (deleted on the first successful read): the map
  only needs to bridge "process died" to "health monitor's next tick",
  never a permanent record.
- Every place that observes a dead process group — `checkStartProcess`,
  the running-state liveness check (`handleLivenessFailure`), and the
  unknown-state recovery check (`checkUnknownProcess`) — routes through one
  `handleDeadProcessGroup` dispatcher instead of calling `markProcessFailed`
  directly. It checks whether a clean (code 0) exit was captured and, if so,
  records the existing `Stopped` state instead — no new `ProcessState`
  value, no restart, no "died"/"not running" log line — otherwise it calls
  `markProcessFailed` with the caller's message/level exactly as before.
  Which state a short-lived process is caught in is a timing accident (a
  build finishing in a second or two can be observed from Starting,
  Running, or Unknown depending only on which tick catches it), so the
  guard has to be a property of "a dead group was observed" as a whole, not
  of any one state-specific branch.
- No exit code known yet (reaper hasn't run) or a nonzero/signal exit is
  treated exactly like today: falls through to the existing Failed path.
  Absence of information is never read as "probably clean."

## Rejected

- **A new `type: oneshot`/`type: build` service kind in `service.yaml`**:
  the right long-term shape for "run once, no server", but a config-schema
  and CLI-surface change, out of scope for fixing a logging/restart
  misclassification. The exit-code check is a strict subset of what that
  type would need anyway.
- **Relying on the standalone daemon's existing SIGCHLD reaper**
  (`internal/process/daemon.go`'s `Wait4(-1, WNOHANG)` loop, which already
  writes `Stopped`/`Failed` by exit code): it only exists for the
  supervised-daemon deployment mode, races unsynchronized against the
  health monitor's own ticker with no shared lock or ordering, and loses
  that race to `captureIdentity`'s own `cmd.Wait()` almost every time in
  practice — effectively dead code for eos-launched services already.
- **Marking every exit-0 death Stopped regardless of whether an exit code
  was actually captured**: would misread the common "reaper hasn't run
  yet" gap as a clean exit. Requiring `ok=true` keeps the change strictly
  additive — a process that would have been marked Failed before still is,
  unless a real exit code says otherwise.
- **Repeating the clean-exit `if` at each of the three call sites**: the
  first version of this change did exactly that for two of the three, and
  left the third (`checkUnknownProcess`) calling `markProcessFailed`
  directly — the same defect the change exists to fix, reachable by a
  different path. An invariant that has to be copy-pasted correctly at
  every call site, forever, is not durable; `handleDeadProcessGroup` makes
  it structural instead — a new dead-group call site has nothing to forget
  because there is no separate check to remember.

## Consequences

A one-shot build command now completes once, logs a single "completed"
line, and sits `Stopped` — no restart loop, no repeated full rebuilds. A
genuine crash (nonzero exit, killed by signal, or death observed before the
reaper reports back) is unaffected: it still logs "died"/"is not running"
and restarts on the existing backoff schedule. The consume-once exit-code
map stays bounded by in-flight deaths, not total restarts over the
daemon's lifetime.
