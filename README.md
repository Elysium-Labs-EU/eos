# eos - Service Supervisor

[![Codeberg](https://img.shields.io/badge/Codeberg-eos-blue?logo=codeberg)](https://codeberg.org/Elysium_Labs/eos)

![eos demo](demo/eos-demo.gif)

Lightweight service supervisor for your VPS. Register services, start them, keep them running.

## Features

* **Auto-restarts crashed processes** with exponential backoff, up to a configurable restart limit.
* **Memory enforcement**, warns at soft thresholds, restarts at hard limits.
* **Log rotation** out of the box; tail logs live with `eos logs --follow`.
* **Boot persistence** via systemd (Linux), system-wide or per-user, generates fitting unit file.
* **Zero runtime dependencies** single static binary.

If you've used PM2 and want something smaller and self-contained, eos covers the core workflow.

## Install

**curl**
```bash
curl -sSL https://codeberg.org/Elysium_Labs/eos/raw/branch/main/install.sh -o install.sh
sudo bash install.sh
```

**wget**
```bash
wget https://codeberg.org/Elysium_Labs/eos/raw/branch/main/install.sh
sudo bash install.sh
```

**From source**
```bash
git clone https://codeberg.org/Elysium_Labs/eos
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
| `eos restart <name>` | Restart a service |

`eos system` covers boot startup, updates, uninstall, and version — run `eos system --help` for the full list.

## Service Configuration

Each service needs a `service.yaml` (or `service.yml`) in its directory.

Minimal:

```yaml
name: "my-service"
command: "/home/user/start.sh"
```

With all options:

```yaml
# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json
name: "cms"
command: "/home/user/start.sh"
port: 1337
env_file: "/home/user/.env"
memory_limit_mb: 200
runtime:
  type: "nodejs"
  path: "/usr/local/bin"
```

## Boot-time Startup

On Linux, `eos system startup` installs a systemd unit and enables it on boot.

```bash
sudo eos system startup   # system-wide unit (runs as invoking user)
eos system startup        # per-user unit (no root required)
```

For user units without a persistent login session:

```bash
loginctl enable-linger <username>
```

Remove with `eos system unstartup`.

## Configuration

eos reads `~/.eos/config.yaml` on startup. All fields are optional.

```yaml
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

Environment variables take precedence over defaults: `EOS_BASE_DIR`, `EOS_INSTALL_DIR`, `EOS_SYSTEMD_TARGET_DIR`, `EOS_VERBOSE`.

## License

Apache License 2.0 - see [LICENSE](LICENSE).
