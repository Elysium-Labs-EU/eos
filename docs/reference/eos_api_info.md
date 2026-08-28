## eos api info

Return service information as JSON

### Synopsis

Return detailed information about a registered service including its process state, runtime statistics, log file paths, and full configuration.

Output schema (stdout, JSON):
  {
    "name":          string           -- service name
    "path":          string           -- absolute path to the service directory
    "config_file":   string           -- absolute path to the service config file
    "created_at":    string (RFC3339) -- when the service was registered
    "log_path":      string|null      -- absolute path to the stdout log file
    "error_log_path":string|null      -- absolute path to the stderr log file
    "config": {
      "command":  string  -- command used to start the service
      "port":     int     -- port the service listens on (omitted if unset)
      "runtime": {
        "type": string    -- runtime identifier (e.g. "nodejs")
        "path": string    -- path to the runtime binary
      }
    },
    "instance": { ... } | null        -- present when the service is running
    "process":  { ... } | null        -- most recent process history entry, plus:
      "orphaned_pgids": []int|omitted -- live process groups left behind by earlier instances
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api info <service-name> [flags]
```

### Examples

```
  eos api info myservice
  eos api info myservice | jq '.config.port'
  eos api info myservice | jq '.process.status'
  eos api info myservice | jq '{name,path,log_path}'
```

### Options

```
  -h, --help   help for info
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

