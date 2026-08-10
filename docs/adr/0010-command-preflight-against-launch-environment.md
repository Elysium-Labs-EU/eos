# 0010. Preflight the command's binary against the launch environment

Date: 2026-08-10
Status: Accepted

## Context

- Since the CLI-through-daemon-socket routing fix, a supervised install's
  daemon launches every service, not the CLI. `buildEnvironment` starts from
  the launching process's own `os.Environ()`, which under systemd has no
  shell profile applied — anything installed by nvm, asdf, volta, or a
  similar per-user tool manager is invisible there even though it works from
  an interactive shell.
- That routing fix is correct and stays. The rule it quietly established was
  never written down: a service runs in the environment of whatever process
  actually launches it, never the environment it was registered in.
- `validateRuntimeBinary` only checks the declared runtime binary
  (`node`/`bun`/`deno`); it says nothing about the command that actually
  runs. A config with `type: node` and a working `runtime.path` still says
  nothing about `npm`, which is the binary that failed on a real host: two
  days of downtime and hundreds of restarts per service, with the cause
  never named anywhere in eos output or a diagnose bundle.
- Local mode (`eos run --no-daemon`) launches a service with the CLI's own
  login-shell environment, so the identical config "works" there. That
  success teaches the wrong lesson before the daemon ever gets a chance to
  disagree.

## Decision

- Preflight `config.Command`'s binary against the exact environment
  `buildEnvironment` would launch it in, right next to `validateRuntimeBinary`,
  at the three places a service can actually start: `StartService`,
  `RestartService`, and `reloadLaunchIncoming`. Same choke point already used
  for the runtime-binary check, so no caller can skip it.
- Only fire when the command is certainly a single simple invocation
  (`FirstCommandBinary`): optional leading `VAR=value` assignments, then one
  bare binary name. Bail out silently on anything else — pipelines, chains,
  subshells, quoting, variable expansion, a path-qualified command — because
  a false failure on a working service costs more than the bug this closes.
- The error names the missing binary, states it was not found in the
  environment this service is about to launch in, and — when found in a
  conventional per-user tool directory (nvm/volta/asdf for node; each
  runtime's own installer for bun/deno) — gives the exact `runtime.path` line
  that fixes it.
- In local mode, warn rather than block: a command that resolves against the
  invoking shell's PATH proves nothing about the daemon's. The warning only
  needs the "no daemon to compare against" wording — see Rejected below for
  why the other half of this (compare two live PATHs) never applies today.

## Rejected

- **Capture the invoking shell's PATH at registration and store it with the
  service**: removes the break entirely, but reintroduces exactly the
  ambient-state dependency the daemon-routing fix existed to remove, and a
  captured PATH goes stale silently the moment the host's toolchain changes.
- **Query a live daemon's PATH over the socket to compare against local
  mode's**: `refuseLocalWrite` already refuses any local write (including a
  local start) while a daemon answers on the socket, so a local start is
  never in a position to make that comparison — the RPC would exist only to
  serve a branch that can't be reached. Worth revisiting only if that refusal
  itself changes.
- **Naive first-token parsing of `command`**: breaks `VAR=1 npm start`,
  `cd www && npm start`, and anything piped or chained — rejected in favor of
  bailing out whenever the shape isn't certain rather than guessing.

## Consequences

- A shell-only PATH addition now fails loud at start/restart/reload with a
  named binary and a concrete fix, instead of a spawn that loops silently.
- Registration (`eos add`, `eos validate`) still can't catch this: the check
  needs the launch environment, which only exists at the three start call
  sites, so a bad config still only surfaces the first time something tries
  to start it.
- Writing the suggested `runtime.path` line into `service.yaml` automatically
  stays a separate, later decision — it edits a user file and deserves its
  own review.
