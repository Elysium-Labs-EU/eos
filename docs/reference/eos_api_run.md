## eos api run

Start or restart a service; always outputs JSON

### Synopsis

Start a named service or register-and-start from a service file.

If the service is already running it is restarted, unless --once is set.

Output schema (stdout, JSON):
  {
    "name":      string  -- service name
    "pgid":      int     -- process group ID of the running service
    "restarted": bool    -- true if service was already running and got restarted
    "skipped":   bool    -- true if --once was set and service was already running
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api run [-f <file>] [--once] [name] [flags]
```

### Examples

```
  eos api run myservice
  eos api run -f ./service.yaml
  eos api run --once myservice
  eos api run myservice | jq .pgid
```

### Options

```
  -f, --file string   path to service.yaml file
  -h, --help          help for run
      --once          do nothing if service is already running
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

