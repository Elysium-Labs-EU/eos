## eos api daemon logs

Return daemon logs as JSON

### Synopsis

Return the last N lines of the daemon log as a JSON array.

Standalone daemon only. For systemd-managed daemons, use journalctl directly:
  journalctl -u eos -n 300

Output schema (stdout, JSON):
  {
    "log_path": string    -- absolute path to the daemon log file
    "lines":    []string  -- log lines, oldest first
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api daemon logs [flags]
```

### Examples

```
  eos api daemon logs
  eos api daemon logs --lines 50
  eos api daemon logs | jq '.lines[-1]'
```

### Options

```
  -h, --help        help for logs
      --lines int   number of lines to return (default 300)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api daemon](eos_api_daemon.md)	 - Machine-readable daemon interface

