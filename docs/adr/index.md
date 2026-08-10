# ADR Index

| # | Title | Status |
|---|-------|--------|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-degraded-mode-e2e-harness.md) | Degraded-mode e2e testing: extend the existing harness | Accepted |
| [0003](0003-crash-reason-line-selection.md) | Crash-reason line selection: bound by pgid, prefer an error-shaped line | Accepted |
| [0004](0004-sink-protocol-version-negotiation.md) | Sink plugin protocol version negotiation: tolerant, not strict | Accepted |
| [0005](0005-clean-exit-vs-crash-during-health-checks.md) | Distinguish a clean exit-0 from a crash during health checks | Accepted |
| [0006](0006-sustained-failure-loop-status.md) | Sustained failure loop: keep retrying, make the state legible | Accepted |
| [0008](0008-daemon-and-service-environment-in-diagnose.md) | Collect the daemon's own environment in diagnose, allowlist-redacted | Accepted |

Find the right one for a change: `make adr-find Q="daemon liveness"` (or any keyword). It greps this directory and asks GitNexus for related code, so you don't have to read every file.
