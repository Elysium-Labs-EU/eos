# 0002. Degraded-mode e2e testing: extend the existing harness

Date: 2026-08-09
Status: Accepted

## Context

Daemon-down and degraded states surfaced a class of bug existing tests did not catch: CLI commands silently showing stale state while the daemon was down, a service wedging in "starting" with no daemon running, stale `updated_at` not flagged, a non-RFC3339 timestamp persisted, and the daemon restart-looping on a permission error reading its logfile. Root cause pattern: the managed service stopped, the CLI read the database directly, and nothing checked staleness.

## Decision

Extend the existing two-tier harness (`cmd/daemon_e2e_test.go`, integration build tag, real binary plus socket poll, run by the integration-test CI job) instead of building new test infrastructure. Add a required or nightly Linux VM tier so systemd-dependent tests stop silently skipping in CI (no PID 1 systemd on existing runners was producing a false green). Add a CI gate, not a PR checklist, plus one CLI/daemon concurrency test.

## Rejected

- **testscript**: fights the daemon's `os.Executable()` re-exec, leaks detached grandchild processes, collides with existing `TestMain`/goleak, breaks on the macOS socket path limit.
- **Formal state x state x command coverage matrix**: opaque fixtures, regression theater, coverage without real signal.

## Consequences

The permission-error logfile case still cannot repro without a real Linux VM (root-owned file, cannot reliably reproduce as non-root), so the VM tier is required, not optional. The actual risk being fixed was the silent skip on non-systemd CI runners, not a lack of raw test count.
