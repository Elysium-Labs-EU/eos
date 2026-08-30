## eos api daemon stop

Stop the running daemon; always outputs JSON

### Synopsis

Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.

Output schema (stdout, JSON):
  {
    "stopped": bool  -- true if a running daemon was stopped, false if it was not running
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api daemon stop [flags]
```

### Examples

```
  eos api daemon stop
  eos api daemon stop | jq .stopped
```

### Options

```
  -h, --help   help for stop
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api daemon](eos_api_daemon.md)	 - Machine-readable daemon interface

