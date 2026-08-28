## eos api status

Return status of all services as JSON

### Synopsis

Return the status of all registered services as a JSON array.

Output schema (stdout, JSON):
  {
    "services": [
      {
        "name":          string           -- service name
        "status":        string           -- current status
        "pgid":          int              -- process group ID (0 if not running)
        "memory_mb":     string           -- memory usage
        "cpu":           string           -- CPU usage percent (e.g. "12.5%")
        "uptime":        string           -- human-readable uptime
        "restart_count": int              -- number of restarts
        "started_at":    string|omitted   -- RFC3339 start time
        "error":         string|omitted   -- last error if any
        "waiting_for":   []string|omitted -- depends_on names still not ready (status "waiting" only)
        "orphaned_pgids":[]int|omitted    -- live process groups left behind by earlier instances
      }
    ]
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api status [flags]
```

### Examples

```
  eos api status
  eos api status | jq '.services[] | select(.status == "running")'
  eos api status | jq '[.services[].name]'
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

