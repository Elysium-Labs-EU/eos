## eos api logs

Return service logs as JSON

### Synopsis

Return the last N lines of a service log as a JSON array.

Output schema (stdout, JSON):
  {
    "name":     string    -- service name
    "log_path": string    -- absolute path to the log file
    "lines":    []string  -- log lines, oldest first
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

Note: --follow is not supported in the API version; use the log_path to tail directly.

```
eos api logs <service-name> [flags]
```

### Examples

```
  eos api logs myservice
  eos api logs myservice --lines 50
  eos api logs myservice --error
  eos api logs myservice | jq '.lines[-1]'
```

### Options

```
      --error       return error log instead of output log
  -h, --help        help for logs
      --lines int   number of lines to return (default 300)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

