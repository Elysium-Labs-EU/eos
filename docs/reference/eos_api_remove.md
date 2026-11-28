## eos api remove

Unregister a service; always outputs JSON

### Synopsis

Unregisters a service and removes its instance record. Does not stop a running process.

Output schema (stdout, JSON):
  {
    "name":    string  -- service name
    "removed": bool    -- true if the catalog entry was removed
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api remove <service-name> [flags]
```

### Examples

```
  eos api remove myservice
  eos api remove myservice | jq .removed
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

