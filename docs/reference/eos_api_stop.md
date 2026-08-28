## eos api stop

Stop a service; always outputs JSON

### Synopsis

Stop all processes for a registered service.

Output schema (stdout, JSON):
  {
    "name":    string  -- service name
    "stopped": int     -- number of processes stopped
    "force":   bool    -- true if --force was used
  }

Error schema (stderr, JSON):
  {
    "error": string  -- human-readable message
    "code":  string  -- present on some failures; a stable, script-matchable
                         reason. "grace_period_exceeded" means the process(es)
                         are still alive and a retry with --force is required
                         to actually kill them.
  }

Exit codes:
  0  success
  1  error

```
eos api stop <service-name> [flags]
```

### Examples

```
  eos api stop myservice
  eos api stop myservice --force
  eos api stop myservice | jq .stopped
```

### Options

```
      --force   force kill immediately
  -h, --help    help for stop
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

