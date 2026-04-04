# eos - Service Orchestration Tool

Run and manage background services on your VPS without the overhead. eos is a lightweight process manager written in Go: register services, start them, and keep track of what's running. No Node.js runtime required, no global daemon to maintain.

If you've used PM2 or similar tools and want something smaller and self-contained, eos covers the core workflow.

## Usage

### Quick Install

Using curl
```bash
curl -sSL https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/install.sh -o install.sh
sudo bash install.sh
```

Using wget
```bash
wget https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/install.sh
sudo bash install.sh
```

### Manual Installation

If you prefer to build from source:
```bash
# Clone and build
go build -o eos

# Database and config automatically created at ~/.eos/
```

### Quick Reference
```bash
# See all commands
./eos --help

# Check database contents (requires eos to have run before)
sqlite3 ~/.eos/state.db "SELECT * FROM service_catalog;"
sqlite3 ~/.eos/state.db "SELECT * FROM service_instances;"
sqlite3 ~/.eos/state.db "SELECT * FROM process_history;"

# Fresh start (delete all registered services)
rm ~/.eos/state.db
```

### Commands

#### Register a Service
```bash
# Register from specific YAML file
./eos add ./path/to/project/service.yaml
```

**Expected:** Service registered in SQLite database at `~/.eos/state.db`

#### List All Services
```bash
./eos status
```

**Shows:** All registered services with their current config (loaded live from filesystem)

#### Run a Service
```bash
# Start or restart a registered service by name
./eos run <service-name>

# Register and start a service from a file in one step
./eos run -f ./path/to/service.yaml

# Start only if not already running (no restart)
./eos run --once <service-name>
```

**Expected:** Service started (or restarted) via daemon. If the service is already running, it will be restarted automatically unless `--once` is set. Using `-f` will register the service if it hasn't been registered yet, then start it.

### Service Configuration File

Each service needs a `service.yaml` (or `service.yml`) file:
```yaml
name: "cms"
command: "/home/user/start-script.sh"
port: 1337
runtime:
  type: "nodejs"
  path: "/opt/homebrew/bin"
memory_limit_mb: 200
```

The CLI finds this file automatically when you register a directory.

## Architecture

### Key Design Decision: Hybrid Approach

**Database for Registry (Fast Discovery)**

- SQLite stores: service name, path, config file location
- Purpose: O(1) lookup without filesystem scanning
- Single source of truth for "what services exist"

**Filesystem for Live Config (Always Current)**

- Service details read from `service.yaml` on each status check
- Purpose: No state drift - config changes are immediately reflected

### Data Flow
```
add command:
  1. Parse service.yaml
  2. Store name + path in SQLite registry
  3. Done

run command:
  1. If -f is given: parse service.yaml and register if not already registered
  2. Look up service in SQLite registry
  3. Start service via daemon; restart if already running
  4. Display result with PGID

status command:
  1. Read registry from SQLite (what services exist?)
  2. For each service, read service.yaml from stored path (current config)
  3. Check if process actually running
  4. Display results
```

## What's Next?
Soon there will be issues listed to show a roadmap and to enable everyone to contribute to this project.

## Releasing

Releases are driven by git tags. The build pipeline stamps the version, commit, and date into the binary automatically.

**Prerequisites:** ensure all changes are committed and tests pass.
```bash
make ci
```

**Tag and push:**
```bash
make release TAG=v1.2.0
```

This creates an annotated git tag and pushes it to origin. Follow [semantic versioning](https://semver.org): `v<major>.<minor>.<patch>`.

**Verify the version:**
```bash
./bin/eos version
# v1.2.0 (commit: a1b2c3d, built: 2026-02-17T10:00:00Z)
```

To test release builds locally without publishing:
```bash
make release-local
# Outputs binaries to ./dist/
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.