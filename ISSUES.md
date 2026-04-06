# Issue Backlog

---

## Bug Fixes

### 1. fix: SQLITE_BUSY on process history update — add retry or reconciliation

**Labels:** `bug`

**Body:**
When a zombie process is reaped, the daemon attempts to update its process history entry. Under load this can race with other writes and produce:

```
[2026-02-27T10:25:39.054Z] ERROR: updating the reaped process in the database: could not update process history entry: database is locked (5) (SQLITE_BUSY)
```

**Expected behavior:** The update either retries with backoff or is deferred until the lock clears.

**Proposed solution:**
Add a reconciliation step — either in `handleSIGCHLDRequest` (retry with exponential backoff) or in `eos status`/`eos info` by cross-checking PIDs in "running" state against live processes and correcting stale records automatically. Stale data self-heals without user intervention.

---

### 2. fix: process type "unknown" in health monitor causes false "died during startup"

**Labels:** `bug`

**Body:**
When a process transitions through or gets stuck in the `unknown` state, the health monitor does not handle it correctly. Restarting a service that is in `failed` or `unknown` state always surfaces "died during startup" even when the service eventually starts successfully.

**Expected behavior:** The health monitor should treat `unknown` as a transient state and wait for the next health tick before classifying the process as failed.

---

### 3. fix: remove PATH dependency on `node` binary in tests

**Labels:** `bug`, `good first issue`

**Body:**
Several tests rely on `node` being present in the system PATH. This makes tests fail in CI environments or machines without Node.js installed, and breaks the `go test ./...` guarantee of zero system dependencies.

**Proposed solution:** Use a self-impersonating test binary (via `GO_TEST_HELPER_PROCESS`) as a fake runtime, and inject it through an `Executor` struct rather than hardcoding `exec.LookPath("node")`.

```go
// Instead of:
cmd := exec.Command("node", args...)

// Use:
type Executor struct {
    NodeBin string // defaults to "node", overridden in tests
}
func (e *Executor) bin() string {
    if e.NodeBin != "" { return e.NodeBin }
    return "node"
}
```

This is the same pattern used by `os/exec` and popular CLI libraries in the stdlib.

---

## Features

### 4. feat: reset restart counter after N minutes of successful uptime

**Labels:** `enhancement`

**Body:**
The restart counter on a service instance currently grows unbounded. If a service crashes once after running for days, it looks like a recurring failure.

**Proposed solution:** Reset `restart_count` to 0 after the service has been in the `running` state for a configurable period (default: 15 minutes). This matches the behavior users expect from tools like pm2 and supervisord.

**Acceptance criteria:**
- Restart count resets after the service has been continuously running for the configured window
- The threshold is configurable (default 15 min)

---

### 5. feat: add shell script runtime support

**Labels:** `enhancement`

**Body:**
eos currently supports `nodejs`, `bun`, and `deno` runtimes. Shell scripts (`.sh`) are a common use case — for example, wrapping a compiled binary or a startup sequence.

**Proposed solution:** Add `shell` as a supported runtime type. When `runtime.type = "shell"`, run the command via `/bin/sh` (or the system default shell).

**Acceptance criteria:**
- `runtime.type: "shell"` works in `service.yaml`
- Shell scripts are started, monitored, and restarted correctly
- Tests cover the new runtime (using the fake-binary injection pattern)

---

### 6. feat: add Python runtime support

**Labels:** `enhancement`

**Body:**
Python is a widely used runtime for background workers, bots, and scripts. Adding first-class support would make eos useful for a broader range of services.

**Proposed solution:** Add `python` as a supported runtime type. Validate the binary path similar to `bun`/`deno`. Support both `python3` and `python`.

**Acceptance criteria:**
- `runtime.type: "python"` works in `service.yaml`
- `runtime.path` is validated for a `python`/`python3` binary
- Tests cover the new runtime

---

### 7. feat: boot persistence — generate systemd/launchd unit (eos startup)

**Labels:** `enhancement`

**Body:**
After a VPS reboot, the eos daemon and all services need to be restarted manually. pm2 solves this with `pm2 startup` which generates a systemd unit.

**Proposed solution:** Add an `eos startup` command that:
1. Detects the init system (systemd, OpenRC, launchd)
2. Prints the unit file and the commands to install it
3. Optionally installs with `--install` flag (requires sudo)

Similar to pm2:
```
eos startup
# [eos] Detected: systemd
# [eos] Copy and run:
# sudo env PATH=... eos startup systemd --install
```

**Acceptance criteria:**
- Works on systemd (primary target)
- Printed instructions are clear and copy-pasteable
- A companion `eos unstartup` removes the generated unit

---

### 8. feat: cron_restart field in service.yaml

**Labels:** `enhancement`

**Body:**
Some services benefit from scheduled restarts (e.g., nightly at 3am to flush memory leaks).

**Proposed solution:** Add a `cron_restart` field to `service.yaml`:

```yaml
cron_restart: "0 3 * * *"
```

The monitor loop stores the next fire time alongside the service instance in the DB. When `NextRestartAt` is in the past, the monitor restarts the service and recomputes the next fire time.

**Acceptance criteria:**
- Field parses and validates as a cron expression
- Monitor restarts the service at the scheduled time
- `eos status` shows next scheduled restart

---

### 9. feat: log pipeline — chain scripts on service stdout

**Labels:** `enhancement`

**Body:**
Some services produce noisy or unstructured logs that benefit from filtering or transformation before being written to disk.

**Proposed solution:** Add a `scripts` field to `service.yaml`:

```yaml
scripts:
  - "/usr/local/bin/add-timestamp"
  - "/usr/local/bin/filter-noise"
```

On spawn, create an `os.Pipe()` per script, set the process stdout → first pipe write end, chain each script (stdin = previous pipe read, stdout = next pipe write), and wire the last script's stdout to the log file.

**Acceptance criteria:**
- Scripts field is optional; no scripts = current behavior
- Each script in the chain receives the previous script's stdout as stdin
- Script failures are logged but do not crash the service

---

### 10. feat: convert pm2 ecosystem.config.js to service.yaml

**Labels:** `enhancement`

**Body:**
Users migrating from pm2 typically have an existing `ecosystem.config.js`. A conversion command would dramatically reduce migration friction.

**Proposed solution:**
```
eos convert ecosystem.config.js
```
Reads the pm2 ecosystem file and outputs one `service.yaml` per service entry to stdout or to a specified directory.

**Acceptance criteria:**
- Handles common pm2 fields: `name`, `script`, `args`, `env`, `max_memory_restart`, `cwd`
- Unknown fields are warned about, not silently dropped
- Works with the standard `module.exports = { apps: [...] }` format

---

### 11. feat: per-service RSS memory tracking

**Labels:** `enhancement`

**Body:**
`ProcessHistory` has an `rss_memory_kb` field but all processes in a group share one entry. For services with multiple child processes, tracking the total RSS per PGID would give a more accurate picture.

**Proposed solution:** Track `{pgid, rss_kb}` pairs individually. The health monitor updates each service's process history with its PGID-summed RSS on each tick. `eos info <service>` can then show a memory history.

**Acceptance criteria:**
- RSS is polled per PGID on each health tick
- Values are stored in process history
- `eos info <service>` shows current and peak RSS

---

### 12. feat: suggest shell completions in install.sh

**Labels:** `enhancement`, `good first issue`

**Body:**
Tab completion for service names is already implemented via `cobra`'s shell completion. The install script should inform users how to enable it.

**Proposed solution:** Add a "Next steps" section to the install script output:

```
To enable tab completion:
  bash:  eos completion bash > /etc/bash_completion.d/eos
  zsh:   eos completion zsh > "${fpath[1]}/_eos"
  fish:  eos completion fish > ~/.config/fish/completions/eos.fish
```

**Acceptance criteria:**
- Completion hint printed at end of `install.sh`
- Instructions for bash, zsh, and fish

---

### 13. fix: env_file field in service.yaml is parsed but never loaded

**Labels:** `bug`

**Body:**
`ServiceConfig.EnvFile` is defined in the struct and accepted in `service.yaml`, but it is never read when the process is started. `local_manager.go` builds the child process environment from `os.Environ()` only — the referenced `.env` file is silently ignored.

**Expected behavior:** When `env_file` is set, the referenced file is parsed and its key=value pairs are injected into the process environment before start.

**Acceptance criteria:**
- `.env` file is read and parsed at service start
- Variables from `env_file` are merged into the process environment
- A missing or unreadable `env_file` returns a clear error (not a silent no-op)
- `eos validate` reports if `env_file` path does not exist

---

### 14. feat: eos env — manage per-service environment variables

**Labels:** `enhancement`

**Body:**
Currently, environment variables must be managed via an external `.env` file referenced in `service.yaml`. There is no way to inspect or override env vars from the CLI.

**Proposed solution:**
```
eos env <service>           # list current env vars (from env_file)
eos env <service> set KEY=VALUE   # write to env_file or service-managed store
eos env <service> unset KEY
```

**Acceptance criteria:**
- `eos env <service>` prints the resolved env for a service
- Variables are sourced from `env_file` if set
- Does not require a service restart to inspect

---

### 14. feat: OpenTelemetry support

**Labels:** `enhancement`, `discussion`

**Body:**
For users running eos in production, exporting metrics and traces to an OpenTelemetry collector would integrate eos into existing observability stacks.

**Proposed solution:** Add an optional `otel` section to the daemon config:

```yaml
otel:
  endpoint: "http://localhost:4317"
  service_name: "eos"
```

Emit basic metrics: process start/stop events, restart count, RSS memory, uptime.

**Acceptance criteria:**
- OTel export is opt-in and disabled by default
- Works with a standard OTLP gRPC endpoint
- Config documented

---

### 15. feat: add eos validate command

**Labels:** `enhancement`, `good first issue`

**Body:**
Currently, a malformed `service.yaml` only errors at `eos add` or `eos run` time. A dedicated validate command would let users check their config before registering.

**Proposed solution:**
```
eos validate ./path/to/service.yaml
```

Parses and validates the file against the same rules used by `LoadServiceConfig`, including runtime path validation. Prints a success message or structured errors.

**Acceptance criteria:**
- Validates all required fields
- Validates `runtime.path` points to a valid directory with the expected binary
- Exits with code 0 on success, 1 on validation error
- Works without the daemon running

---

### 16. feat: multi-user daemon architecture

**Labels:** `enhancement`, `discussion`

**Body:**
Currently each user on a VPS has their own daemon. When a superuser updates eos, they may want to restart all user daemons. The current model makes that difficult.

**Open question:** Should we keep per-user isolation (current model) and add a mechanism for superusers to signal all daemons? Or move to a single system-level daemon that handles per-user service isolation?

**Context:**
- Per-user daemons provide better isolation and are simpler
- A system daemon would be easier to manage across updates but requires privilege escalation design

This issue is for discussion before implementation.

---

## Improvements

### 17. improve: add JSON Schema for service.yaml with YAML language server support

**Labels:** `enhancement`, `good first issue`

**Body:**
The `schemas/service.schema.json` file has been added to the repo. Editors that support the YAML language server can validate `service.yaml` files automatically when users add the schema comment.

**Remaining work:**
- Publish schema at a stable URL (via Codeberg raw file URL)
- Document schema usage in README and in `examples/service.yaml`
- Consider registering with [SchemaStore](https://www.schemastore.org/json/) so editors pick it up automatically without a manual comment

**Acceptance criteria:**
- Schema URL is documented
- SchemaStore PR submitted (or noted as future work)

---

### 18. improve: versioned HTTP API

**Labels:** `enhancement`

**Body:**
The existing API routes (`/api/...`) are not versioned. As the API evolves, breaking changes will affect any tooling built on top.

**Proposed solution:** Prefix all API routes with `/api/v1/`. Existing routes remain unchanged until a v2 is introduced.

**Acceptance criteria:**
- All routes accessible under `/api/v1/`
- Old routes either redirect or are documented as deprecated

---

### 19. improve: increase platform configurability

**Labels:** `enhancement`

**Body:**
The root command has a `config` function but most daemon settings cannot be changed without recompiling. Users on memory-constrained VPS instances in particular would benefit from tunable defaults.

**Proposed solution:** Expose key settings in `~/.eos/config.yaml`:
- Health check interval
- Restart backoff
- Log retention (max files, max size)
- Memory limit enforcement behavior

**Acceptance criteria:**
- Config file is read at daemon startup
- Documented with examples
- Sensible defaults require no config file to be present
