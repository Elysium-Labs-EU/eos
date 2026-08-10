# 0006. Sustained failure loop: keep retrying, make the state legible

Date: 2026-08-10
Status: Accepted

## Context

A service whose start command fails deterministically restarts forever at the
backoff ceiling. `RestartService` succeeds (the shell spawns fine), the child
exits immediately after, so `handleRestartFailure`'s halt path — which only
fires on an `os.ErrPermission` from `RestartService` itself — is never
reached. Nothing distinguishes this from a service that is merely flaky, and
every attempt logs three lines. On a real host this produced 933 identical
restarts over roughly two days, filling the error log so completely that the
last-good state fell out of the retention window.

## Decision

- Track `FailureLoopCount`/`FailureSignature` on `ServiceInstance`: bump on a
  repeat of the same captured cause, reset on any other cause.
- Once `FailureLoopCount` crosses a threshold, overlay a distinct `crashloop`
  status at render time (`eos status`, `eos api status`, the diagnose
  bundle) — the service is still being retried, this only says the retries
  aren't making progress.
- Widen the restart backoff ceiling from 60s to 5 minutes once in that state.
- Collapse the health monitor's own repeated log breadcrumbs into a periodic
  "repeated N times" summary instead of writing the full set every attempt.
- The existing 15-minute stable-uptime reset that zeroes `RestartCount` also
  zeroes the failure-loop state, so recovery clears both together.

## Rejected

- **Halt on repeated identical failures**: a service blocked on something
  that comes up late (a remote database, a mount, a VPN) emits the identical
  error every time and is indistinguishable from a permanent fault. Halting
  would kill services that were about to recover. `depends_on`/`max_wait`
  cover this for dependencies eos manages itself, not anything outside it.
- **Classify the exit code** (127 command-not-found, 126 not-executable):
  precise, but both conditions can clear on their own (e.g. a PATH fix),
  which is exactly what a code-based halt would treat as permanent. A
  reasonable refinement later, layered on top of this, not instead of it.

## Consequences

Matches Kubernetes' `restartPolicy: Always`: never gives up on its own,
signals the stuck state (`CrashLoopBackOff`) instead of hiding it. eos never
kills a service on the operator's behalf for looking stuck; the retry keeps
running until either the cause clears or someone intervenes.
