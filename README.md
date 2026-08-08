<p align="center">
  <img src=".github/logo.svg" alt="eos logo" width="120" height="120">
</p>

# eos - Service Supervisor

[![GitHub](https://img.shields.io/badge/GitHub-eos-blue?logo=github)](https://github.com/Elysium-Labs-EU/eos)

![eos demo](demo/eos-demo.gif)

Lightweight service supervisor for your VPS. Register services, start them, keep them running.

## Features

* **Auto-restarts crashed processes** with exponential backoff, retrying indefinitely so a service self-heals whenever the underlying cause clears.
* **Memory enforcement**, warns at soft thresholds, restarts at hard limits.
* **Log rotation** out of the box; tail logs live with `eos logs --follow`.
* **Boot persistence** via systemd (Linux) or launchd (macOS), system-wide or per-user, generates fitting unit file.
* **Zero runtime dependencies** single static binary.

If you've used PM2 and want something smaller and self-contained, eos covers the core workflow.

## Install

**curl**
```bash
curl -sSL https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/install.sh -o install.sh
sudo bash install.sh
```

**wget**
```bash
wget https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/install.sh
sudo bash install.sh
```

**From source**
```bash
git clone https://github.com/Elysium-Labs-EU/eos
cd eos
go build -o eos
```

## Quick Start

```bash
# Register a service
eos add ./path/to/project

# Start it
eos run my-service

# Check status of all services
eos status
```

## Commands

| Command | Description |
|---------|-------------|
| `eos add <path>` | Register a service from a directory |
| `eos run <name>` | Start or restart a service |
| `eos run -f <file>` | Register and start from a file in one step |
| `eos run --once <name>` | Start only if not already running |
| `eos status` | Show all services with status, memory, uptime |
| `eos info <name>` | Detailed view: config, logs, process stats |
| `eos logs <name>` | View output logs |
| `eos logs --error <name>` | View error logs |
| `eos logs --follow <name>` | Tail logs in real time |
| `eos stop <name>` | Stop a service |
| `eos reload <name>` | Zero-downtime reload (see below) |

`eos system` covers boot startup, updates, uninstall, and version; run `eos system --help` for the full list.

`eos config` covers viewing, scaffolding, and validating `~/.eos/config.yaml` (see [Configuration](#configuration)); run `eos config --help` for the full list.

## Zero-downtime Reload

`eos reload <name>` swaps a running service for a fresh instance without dropping connections. eos starts the new instance alongside the old one, waits for it to pass the health check, then drains the old one. If the new instance never becomes healthy the old one keeps serving, so a broken deploy is a no-op instead of an outage. This is the difference from `eos run`, which restarts by stopping then starting and drops the listening socket in between.

```bash
eos reload my-service
```

The overlap only works because both instances listen on the same port at the same time, and that is the service's job, not eos's: the service **must** bind its port with `SO_REUSEPORT` and bind promptly on startup. eos never owns the listening socket or proxies traffic; it only sequences the cutover. A service that does not use `SO_REUSEPORT` cannot run two instances on one port, so its reload will abort and leave the old instance untouched. Reload runs through the daemon, so it is unavailable with `--no-daemon`.

## Service Configuration

Each service needs a `service.yaml` (or `service.yml`) in its directory.

Minimal:

```yaml
name: "my-service"
command: "/home/user/start.sh"
```

With all options:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json
name: "cms"
command: "/home/user/start.sh"
port: 1337
env_file: "/home/user/.env"
memory_limit_mb: 200
cron_restart: "0 3 * * *"
log_max_files: 5
log_file_size_limit_bytes: 10485760
runtime:
  type: "nodejs"
  path: "/usr/local/bin"
```

`log_max_files` and `log_file_size_limit_bytes` cap a service's `<name>-out.log`/`<name>-error.log` rotation; both default to the daemon's own log rotation settings (`eos system info`) when unset.

## Boot-time Startup

`eos system startup` installs a systemd unit (Linux) or a launchd plist (macOS) and enables it on boot.

```bash
sudo eos system startup   # system-wide unit / LaunchDaemon (runs as invoking user)
eos system startup        # per-user unit / LaunchAgent (no root required)
```

For systemd user units without a persistent login session:

```bash
loginctl enable-linger <username>
```

Remove with `eos system unstartup`.

## Configuration

eos reads `~/.eos/config.yaml` on startup. All fields are optional.

```yaml
sinks:
  prod-loki:
    type: loki
    mode: push
    address: "http://loki:3100"
telemetry:
  enable: false
  endpoint: ""
  insecure: false
health:
  checkIntervalMs: 2000
  memSampleIntervalMs: 30000
  backoff:
    baseMs: 300
    maxMs: 60000
  memory:
    warningThreshold: 0.75
    softRestartThreshold: 0.85
    forceRestartThreshold: 0.95
log:
  maxFiles: 5
  fileSizeLimitBytes: 10485760
```

Environment variables take precedence over defaults: `EOS_BASE_DIR`, `EOS_INSTALL_DIR`, `EOS_SYSTEMD_TARGET_DIR`, `EOS_VERBOSE`, `HEALTH_CHECK_INTERVAL_MS`, `HEALTH_MEM_SAMPLE_INTERVAL_MS`, `HEALTH_BACKOFF_BASE_MS`, `HEALTH_BACKOFF_MAX_MS`, `HEALTH_TIMEOUT_ENABLE`, `HEALTH_RESTART_COUNTER_RESET_WINDOW`, `SHUTDOWN_GRACE_PERIOD`.

`eos config` manages this file directly, so you don't need to hand-write it from scratch or read this README to know it exists:

```bash
eos config init      # scaffold ~/.eos/config.yaml, fully commented at eos's own defaults
eos config show      # print the effective config: file values merged over defaults
eos config validate  # check the file without starting the daemon
```

## Log Sinks

eos can forward logs to external destinations via sink plugins. Each sink runs as a subprocess: eos pipes JSON log records to its stdin and restarts it if it crashes.

Declare sinks in `service.yaml` under `log_sinks`:

```yaml
log_sinks:
  - type: loki
    mode: push
    address: "http://loki:3100"

  - type: sse
    mode: serve
    address: ":9000"
```

`type` maps to a binary on PATH named `eos-sink-<type>`. Available plugins (Loki, SSE, Logbench) are maintained at [github.com/Elysium-Labs-EU/eos-plugins](https://github.com/Elysium-Labs-EU/eos-plugins).

When multiple services share the same sink, register it once in `~/.eos/config.yaml` and reference it by name instead of repeating the config in every `service.yaml`:

```yaml
# ~/.eos/config.yaml
sinks:
  prod-loki:
    type: loki
    mode: push
    address: "http://loki:3100"
  local-file:
    type: file
    mode: serve
    address: /var/log/eos
```

```yaml
# service.yaml
log_sinks: [prod-loki, local-file]
```

Named references and inline sink configs compose in the same `log_sinks` list. The daemon resolves names at service start; an unknown name is a hard error.

## Deploy with GitHub Actions

The [eos-deploy-action](https://github.com/Elysium-Labs-EU/eos-deploy-action) handles SSH, binary install, and service restart in one step.

```yaml
- uses: docker://ghcr.io/elysium-labs-eu/eos-deploy-action:latest
  with:
    host: ${{ secrets.DEPLOY_HOST }}
    user: ${{ secrets.DEPLOY_USER }}
    ssh_key: ${{ secrets.DEPLOY_SSH_KEY }}
    service: my-service
```

Add it to your release workflow and pushes to `main` deploy automatically.

## License

Apache License 2.0 - see [LICENSE](LICENSE).
