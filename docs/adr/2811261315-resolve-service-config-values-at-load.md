# 2811261315. Resolve service config values once, at load

Date: 2026-11-28
Status: Accepted

## Context

- `runtime.path` reached every consumer as the raw string from
  `service.yaml`. Validation, launch (`buildEnvironment`) and display
  (`eos info`) each interpreted it on their own, and two of them disagreed:
  validation joined a relative path onto `$HOME`, launch put it on `PATH`
  as-is, so the shell resolved it against the service directory and could
  run a different runtime than the one validated.
- A shared helper called by each consumer fixed that pair, but any new
  consumer still had to remember to call it.
- Every `ServiceConfig` comes from `service.yaml` through two loaders,
  `LoadServiceConfig` and `ValidateServiceConfig`; nothing else builds or
  decodes one.

## Decision

- `cfgvLoadAndParseConfig` is the single place a `service.yaml` becomes a
  `ServiceConfig`; both loaders go through it.
- Values that need interpreting are resolved there, once. After loading,
  consumers treat the config as clean data and never re-interpret it.
- `runtime.path` is made absolute by a pure
  `resolveRuntimePath(path, home)`: empty and absolute unchanged, `~` is
  home, `~/x` and a plain relative `x` join onto home, `~user/x` is an
  error.
- Home is the loading process's own (`os.UserHomeDir`), looked up only when
  the path needs it. This follows 0010's rule that a service runs in the
  environment of the process that launches it; the daemon loads the config
  itself right before launching.
- A relative path that cannot be resolved (no home) fails the load, not
  just the launch.
- `ValidateRuntimePath` asserts the path is absolute; a relative value
  there means a config skipped the loader, which is a bug.

## Rejected

- **Resolve in each consumer through a shared helper**: correct today, but
  correctness depends on every future consumer remembering the call.
- **Pass `home` into the loaders from every call site** (STYLE.md's
  "constructors take resolved values" to the letter): fifteen call sites
  each repeating `os.UserHomeDir()`, which is the "ambient lookup
  re-derived at multiple sites" problem STYLE.md warns about. The loader
  already reads the file, so it is the I/O boundary config data enters
  through.
- **Home from `userutil.ResolveIdentity`** (follows `SUDO_USER`): would
  resolve against the invoking user's home under `sudo` while the process
  itself runs with root's environment, departing from 0010's
  launch-environment rule.

## Consequences

- `eos info` and `eos api info` show the resolved absolute path instead of
  what the user wrote; that is what will run.
- A config with a relative `runtime.path` and no resolvable home now fails
  `status`, `info` and health checks for that service, not only its start.
- The CLI and the daemon can still resolve `~` differently when their
  `HOME` differs; that is the launch-environment rule, now applied
  consistently.
- New interpreted config fields belong in `cfgvLoadAndParseConfig`, with a
  pure resolver and a loader test, not in their consumers.
- `findBinaryElsewhereOnHost` still calls `os.UserHomeDir()` separately for
  its hint message; a candidate to take `home` as a parameter.
