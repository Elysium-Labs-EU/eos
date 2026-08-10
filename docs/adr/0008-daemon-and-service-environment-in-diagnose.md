# 0008. Collect the daemon's own environment in diagnose, allowlist-redacted

Date: 2026-08-10
Status: Accepted

## Context

`eos diagnose` collects version, daemon, and per-service status plus logs,
but has no way to show the daemon process's own environment at any flag
setting. `buildEnvironment` (internal/manager) starts every launched
service's environment from `os.Environ()` — the daemon's own environment —
then layers `runtime.path` and the service's env_file on top. That base
layer varies by how the daemon was started (systemd unit, login shell, etc.)
and is the one layer no existing flag exposes. `--include-env` only dumps
each service's configured env_file, a different thing from the environment
a process actually received. Diagnosing a `command not found` failure
currently requires reading eos source to work out where a service's PATH
comes from, because nothing in the bundle answers it directly.

## Decision

- Read the daemon's own environment straight from its live process via
  `/proc/<pid>/environ` (new `procutil.ReadEnviron`), using the pid
  `diagnoseCollectDaemonInfo` already resolves. Written to `daemon-env.json`,
  a top-level bundle file, not nested under any per-service data.
- Also read each running service's resolved PATH the same way, keyed by the
  PGID already tracked in process history (eos launches every service as its
  own process group leader, so PGID doubles as that leader's pid). Written
  to `service-env.json`, so a PATH-shaped failure can be diffed against
  `daemon-env.json` instead of re-derived by hand.
- Redact by allowlist, not by pattern: `PATH`, `HOME`, `USER`, `SHELL`,
  `LANG`, `PWD`, and the variables systemd sets on a spawned process.
  Environment variable names are a fixed, known vocabulary — unlike log
  content, which is arbitrary — so a positive list is possible and
  preferable to a regex-based scrub. A name outside the allowlist is listed
  with its value withheld, never dropped: the bundle still shows the shape
  of the environment without risking a leaked secret.
- Both collectors run unconditionally, not gated behind a flag: a
  withheld-value environment carries no secret, so there is no reason to
  make the reader ask for a second bundle with the right flag just to see
  the daemon's PATH.
- `procutil.ReadEnviron` has no implementation outside Linux (matching
  `platformStartTime`/`platformCPUTime`'s existing per-platform split):
  macOS has no `/proc`, and the equivalent (`KERN_PROCARGS2`) needs
  argv/envp-boundary parsing eos has no reason to build for what is only a
  local-dev target, not a production supervision platform.

## Rejected

- **Pattern-based scrub (reusing `diagnoseScrubLine`) for environment
  values**: works for the log-scrubbing case because log content is
  unstructured and unknowable in advance, but an environment variable is
  `name=value` with a fixed vocabulary of names — an allowlist is strictly
  safer and simpler than trying to pattern-match arbitrary secret shapes in
  values.
- **Dropping non-allowlisted names entirely**: a bundle that silently omits
  unknown variables looks identical to one where nothing else was set,
  hiding real information (e.g. "there are 12 more vars we can't show") that
  costs nothing to reveal as names alone.
- **Gating this behind a new flag, mirroring `--include-env`**: the
  allowlist redaction already makes the output as safe as everything else
  the bundle ships by default; requiring a flag would just reintroduce the
  gap this fix exists to close.
- **Deriving the daemon's environment from the `eos diagnose` process's own
  `os.Environ()`**: diagnose runs as a separate CLI invocation, not the
  daemon process itself, and can be run from a different shell/session than
  the one that started the daemon — reading the live daemon PID's own
  `/proc/<pid>/environ` is the only way to get what the daemon actually has.

## Consequences

A `command not found` failure caused by the daemon's own PATH is now
directly diffable from a single bundle, closing the gap that previously
required reading eos source to explain. `procutil.ReadEnviron` is
Linux-only; a bundle collected on a non-Linux host reports a failed,
non-fatal step for `daemon-env`/`service-env:<name>` rather than blocking
the rest of the bundle, consistent with every other collector's degrade-
independently design.
