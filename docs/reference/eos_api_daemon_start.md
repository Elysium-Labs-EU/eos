## eos api daemon start

Start the daemon; always outputs JSON

### Synopsis

Start the daemon process.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).
Otherwise starts the daemon detached in the background; control returns once the PID file and socket are confirmed live (up to ~16s worst case: each of the two liveness checks allows a 5s primary wait plus a 3s tolerant grace window, and the checks run one after the other). Unlike "eos daemon start", there is no --foreground option — the JSON contract requires the command to return once startup is confirmed, not block for the daemon's lifetime.

Idempotent: if the daemon is already running, returns "started": false with exit code 0 instead of erroring, matching "eos api daemon stop"'s idempotency contract.

Output schema (stdout, JSON):
  {
    "started": bool  -- true on success, false if the daemon was already running
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api daemon start [flags]
```

### Examples

```
  eos api daemon start
  eos api daemon start | jq .started
```

### Options

```
  -h, --help   help for start
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api daemon](eos_api_daemon.md)	 - Machine-readable daemon interface

