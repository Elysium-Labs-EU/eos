## eos api add

Register a service from a directory; always outputs JSON

### Synopsis

Register a service by providing the path to a directory containing a service configuration.

Output schema (stdout, JSON):
  {
    "name":        string  -- service name from config
    "path":        string  -- absolute path to the service directory
    "config_file": string  -- config filename (e.g. service.yaml)
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api add <path> [flags]
```

### Examples

```
  eos api add ./path/to/project
  eos api add ./path/to/project/service.yaml
  eos api add ./path/to/project | jq .name
```

### Options

```
  -h, --help   help for add
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

