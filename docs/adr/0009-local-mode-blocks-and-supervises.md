# 0009. Local mode blocks and supervises instead of launching and returning

Date: 2026-08-10
Status: Accepted

## Context

`--no-daemon` (and, until the manager-selection fix, every command against a
supervised install whose daemon is down) launches a service as a child of
the CLI process via `LocalManager.buildLaunchCommand`, then the CLI returns.
Two properties of that launch assume the launching process stays alive for
as long as the service does — true for the real standalone daemon, false
for a CLI invocation that starts a service and returns almost immediately:

- `prepareLaunchIO` wires the service's stdout/stderr to `os.Pipe()`s
  drained by goroutines inside the launching process. Once that process
  exits, the pipes' read ends close; the service gets EPIPE on its next
  write. `nextjs` (writes a startup banner immediately) and `vite` (a build
  that outlives the CLI) both hit this on `make test-fixtures-orb`; services
  that write nothing before the CLI exits happened not to.
- `buildLaunchCommand` passes `m.ctx` to `exec.CommandContext`. Cancelling
  that context stops the service — correct for the daemon, whose own
  shutdown is exactly when it should, but there is no equivalent shutdown
  moment for a CLI invocation that already returned.

## Decision

Local mode's launching command blocks and supervises the service itself,
in the foreground, instead of launching it and returning:

- `eos run` in local mode prints its usual start banner, then blocks until
  interrupted (SIGINT/SIGTERM) or the service exits on its own — a crash,
  or a separate `eos stop`/`eos api stop` against the same service. On
  interrupt it stops the service gracefully with the same shutdown grace
  period the daemon uses, then exits. `runSuperviseIfLocal` decides whether
  to do this at all: it type-asserts the manager to `*manager.LocalManager`,
  so talking to a live daemon (`*manager.DaemonManager`) is untouched and
  returns immediately exactly as before — the daemon already supervises the
  service on its own.
- Both defects in Context become correct, not just harmless, once the CLI
  is the thing supervising: the pipes are drained by a process alive for
  exactly as long as the service is, by construction, so nothing needs to
  be made durable; cancelling on the launching context now stops the
  service because terminating a foreground supervisor is supposed to do
  that. `buildLaunchCommand`/`prepareLaunchIO` needed **no changes at all**
  — every property that made the pipe-forwarding, JSON-enveloped,
  sink-forwarding, rotated path work for the daemon now also holds for a
  blocking `eos run`, because the process running it is, for the duration,
  a real supervisor.
- `eos api run` refuses a local start instead of blocking. It is a machine
  contract that promises a pgid for a process that will still exist after
  the command exits — a promise blocking can't keep (it would hang every
  script piping the JSON output through `jq`) and fire-and-forget can't
  keep either (the pgid it hands back is for a process about to be
  orphaned). Refusing is the only response that doesn't hand a caller a
  false success. `apiRefuseLocalStart` takes a new `isLocal bool` (the
  manager type-assertion `apiRefuseLocalWrite`'s existing `localMode` can't
  express — see its own doc comment) and refuses whenever it's true,
  regardless of *why* the manager is local, with a structured JSON error
  (exit 1) naming `eos run` as the fix.
- `eos add`/`eos update` are unaffected: they only write catalog rows
  (`refuseLocalWrite`), never start a process.
- `eos reload` is unaffected: it already hard-refuses outside the daemon
  via a `serviceReloader` interface assertion `*manager.LocalManager`
  doesn't satisfy — reload launches a second instance and health-gates the
  cutover inside the supervising daemon, which local mode doesn't have.

## Rejected

- **Give the service its own fds and detach the launch context**
  (durable I/O — the first version of this ADR): make `prepareLaunchIO`
  open real log files directly instead of pipes when the manager is a
  one-shot CLI launcher, and detach `buildLaunchCommand`'s context so nothing
  can cancel it after the CLI returns. This is workable and was
  implemented, but it gives up everything a blocking supervisor keeps for
  free: durable-mode output is raw text, not the daemon's structured JSON
  envelope, so it carries no `service`/`pgid`/`source` fields and
  `GetServiceLastErrorLine` finds no crash reason in it; it isn't forwarded
  to log sinks (a sink subprocess started by a one-shot CLI invocation
  would itself become the exact orphaned-child problem this ADR fixes, one
  level down); and it isn't size-rotated live (rotation is a `Write()`-time
  decision made by whichever process is doing the writing, and in durable
  mode that's the child itself via a plain fd, not this process). A
  foreground supervisor gives up none of this, because the supervisor is
  still there to do the work — this is the strongest argument for blocking
  instead, and it came directly from naming durable mode's losses honestly.
- **Refuse to launch a service that outlives the CLI in local mode, for
  `eos run` too**: rejected for the reason already given when this ADR
  first considered it — `--no-daemon` is supported and the fixture harness
  depends on it, and humans and scripts both have a real use for a
  foreground-supervised local run. This is *not* a reversal of that
  rejection for `eos api run`: that command's documented contract (pgid for
  a process that outlives the command) is unsatisfiable without a
  supervisor no matter how it's invoked, which is a different, narrower
  problem than "local mode needs a supervisor" — `eos run` solves the
  general problem by becoming one; `eos api run` can't become one without
  breaking the JSON-then-exit contract scripts already depend on.
- **A `--detach`/`--background` flag on `eos run`** to keep the old
  fire-and-forget behavior available: rejected in favor of the shell's own
  backgrounding (`eos run svc &`). A flag would recreate exactly the
  orphaning failure mode this ADR exists to close, just behind an opt-in
  instead of by default — there is no way to background `eos run` safely
  without something supervising the result, and the shell already has a
  mechanism for "start this and keep going" that doesn't need eos to
  reinvent it.

## Consequences

`eos run <service>` in local mode no longer returns immediately — this is
a deliberate behavior change, not a pure bug fix. Anything scripted against
the old behavior needs to background it (`eos run svc &`) or switch to a
supervised daemon. In one line each, this is now the definition of what
`--no-daemon` means for every command that can reach it:

- `eos run` blocks and supervises the service in the foreground.
- `eos api run` refuses a local start outright (exit 1, JSON error).
- `eos add`/`eos update` only ever write catalog rows, unaffected.
- `eos reload` still refuses outside the daemon, unaffected.

`make test-fixtures-orb` now backgrounds its `eos run` launches and signals
them to stop instead of relying on a fire-and-forget `eos api run`, which
`eos api run` no longer permits in local mode.
