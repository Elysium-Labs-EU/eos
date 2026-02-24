# eos - Service Orchestration Tool

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

# Check database contents
sqlite3 ~/.eos/state.db "SELECT * FROM registry;"

# Fresh start (delete all registered services)
rm ~/.eos/state.db
```

### Commands

#### Register a Service

```bash
# Register from directory (looks for service.yaml inside)
./eos add ./path/to/project

# Register from specific YAML file
./eos add ./path/to/project/service.yaml
```

**Expected:** Service registered in SQLite database at `~/.eos/state.db`

#### List All Services

```bash
./eos status
```

**Shows:** All registered services with their current config (loaded live from filesystem)

#### Start a Service

```bash
./eos start <service-name>
```

**Expected:** Service started via daemon, will restart if encountering failure.

### Service Configuration File

Each service needs a `service.yaml` (or `service.yml`) file:

```yaml
name: "cms"
command: "/home/user/start-script.sh"
runtime:
  type: "nodejs"
  path: "/opt/homebrew/bin"
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
