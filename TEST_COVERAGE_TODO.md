# cmd test coverage TODO

Derived 2026-07-12 from `go test ./cmd/... -coverprofile` (cmd 61.6%, cmd/helpers 53.2%).
Skip start.go/restart.go — deprecated cmds, removed v0.0.12.

## 1. cmd/helpers/print.go — 0% (easy win)
`PrintStatus`, `PrintSection`, `PrintKV` all untested. Pure stdout funcs, no mocks needed.

## 2. cmd/helpers/helpers.go — gaps
`DetermineUptimeAPI` 0%, `PromptConfirm` 0%, `Debugf` 0%, `PrintSudoHint` 0%, `PrintRequiresSudo` 0%.

## 3. cmd/helpers/completions.go — 0%
`ServiceNameCompletions` untested.

## 4. cmd/status.go — renderWatchFrame 0%
File touched in f12135d but this func still uncovered.

## 5. cmd/daemon.go — biggest gap
`daemon_e2e_test.go` is `//go:build integration` — never runs under plain `go test ./cmd/...`,
which is why standaloneDaemonController shows 0% despite being e2e-covered.

`internal/process` daemon funcs (`StatusStandaloneDaemon`, `StopStandaloneDaemon`,
`RemoveStandaloneDaemon`) are file-state driven (pidfile + socket existence, `Signal(0)`
liveness check) — no root/systemd needed. Real unit coverage is possible:
- not-found: no pidfile -> Running=false, Pid=nil
- stale/dead: pidfile has an already-exited pid -> Running=false, Pid=<n>
- running: write `os.Getpid()` (test process itself) into pidfile + touch socket path ->
  Signal(0) succeeds against self, Running=true, no real daemon binary needed

**Do:** unit-test `standaloneDaemonController` (Start/Stop/Remove/Info/Logs/LogsHint) via
fake pidfile/socket files in `t.TempDir()`, plus `printStandaloneDaemonDetails` and
`printSystemdDaemonDetails` (pure formatting, no exec).

**Skip:** `systemdDaemonController` methods (shell out to real `systemctl`, need root/systemd)
and `forkDaemon`'s success path (execs real binary via `os.Executable()`) — same
Linux+root+systemd exclusion as [[feedback_skip_deprecated_cmds]] and old Priority 3/4.
`newDaemonController` (40%) — cover the two config-branch cases (standalone vs systemd vs
neither-nil-error), cheap.

## 6. cmd/api_daemon_logs.go — newAPIDaemonLogsCmd 25%

## 7. cmd/api_info.go — compileProcessInfoObject 16.7%

## 8. cmd/system.go — scattered 0%
`detectActiveSystemRuntime`, `execRunCmd`, `userRuntimeDir`, `uninstallCmd`,
`handleStoppingServices`.

## 9. cmd/remove.go (42.2%) and cmd/update.go (51.6%) — moderate gaps, revisit after above

## Skip
- cmd/root.go newRootCmd/newManager/Execute — thin entrypoint wiring, e2e territory not unit territory
- start.go / restart.go — deprecated, per user instruction
