# Architecture

eos is a CLI that talks to a long-lived daemon over a Unix socket. This page explains how the pieces fit together, for anyone reading the code for the first time. Individual design decisions and their tradeoffs are recorded separately in `docs/adr/`.

## CLI and daemon

Every `eos` invocation is a short-lived CLI process. Commands that need to act on a running service (`run`, `stop`, `status`, `logs`, ...) don't manage that service directly: they send a request over a Unix socket to the daemon, and the daemon does the work.

The daemon can be reached three ways, chosen automatically per invocation in `newManager` (`cmd/root.go`):

- **Supervised** — a systemd unit, launchd agent, or OpenRC service owns the daemon's lifecycle. The CLI only talks to its socket; it never forks one itself, since the supervisor already restarts it on crash or reboot.
- **Standalone** — eos manages the daemon itself. If the socket doesn't answer, the CLI forks a detached daemon process on demand from the PID file it wrote.
- **In-process (`--no-daemon`)** — no daemon at all. The command opens the state database directly and runs against it in the same process. This is also the automatic fallback when a supervised daemon is configured but currently down, so read commands (`status`, `logs`) keep serving last-known state instead of failing outright.

The daemon itself, once running, owns the process supervision loop: starting registered services, watching health, restarting on crash with backoff, enforcing memory limits, and rotating logs.

## Package map

| Package | Responsibility |
|---|---|
| `cmd/` | Cobra command tree; one file per subcommand, wired together in `cmd/root.go` |
| `internal/manager/` | The `ServiceManager` interface and its implementations (in-process `LocalManager`, daemon-backed managers) |
| `internal/process/` | OS-level process spawning, signal delivery, and stdin/stdout piping for daemons |
| `internal/procutil/` | OS-level process liveness checks shared across packages (process groups, `/proc` reads) |
| `internal/monitor/` | Health checking and automatic restart logic for managed daemons |
| `internal/config/` | Config file and env var resolution (`~/.eos/config.yaml`, `EOS_*` env vars, per-platform daemon supervision config) |
| `internal/database/` | SQLite connection, schema migrations, and query helpers for eos state persistence |
| `internal/types/` | Shared data types, including the plugin/daemon wire contract that `check-plugin-api-diff` guards against breaking changes |
| `internal/userutil/` | Identity resolution (who is running eos, what base dir they get) — see the style guide's rule on not re-deriving ambient facts at multiple sites |

## Log sinks

Services can forward logs to external destinations (Loki, SSE, a plain file) through sink plugins. Each sink runs as its own subprocess named `eos-sink-<type>` on `PATH`; the daemon pipes JSON log records to its stdin and restarts it if it crashes. Sinks are maintained in the separate [eos-plugins](https://github.com/Elysium-Labs-EU/eos-plugins) repository, not in this one — `internal/types` is the contract both sides compile against.

## Where to look next

- **A specific design decision and why it was made** — `docs/adr/`, or `task setup:adr-find Q="concept"`.
- **Exact CLI flags and subcommands** — generated reference at `docs/reference/` (`task docs:gen` to regenerate after touching `cmd/`).
- **Coding conventions** — `STYLE.md`.
