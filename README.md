# eos - Service Supervisor

[![Codeberg](https://img.shields.io/badge/Codeberg-eos-blue?logo=codeberg)](https://codeberg.org/Elysium_Labs/eos)

![eos demo](demo/eos-demo.gif)

Lightweight process manager for your VPS. Register services, start them, keep them running. No Node.js, no global daemon, no overhead.

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

## Service Configuration

Each service needs a `service.yaml` (or `service.yml`) in its directory:

```yaml
name: "cms"
command: "/home/user/start-script.sh"
port: 1337
runtime:
  type: "nodejs"
  path: "/opt/homebrew/bin"
memory_limit_mb: 200
```

## License

Apache License 2.0 - see [LICENSE](LICENSE).
