## eos api validate

Validate a service config file; always outputs JSON

### Synopsis

Validate a service.yaml without registering it or requiring the daemon.

Output schema (stdout, JSON):
  {
    "valid":       bool      -- true if config is valid
    "name":        string    -- service name from config (empty if parse failed)
    "path":        string    -- absolute path to the service directory
    "config_file": string    -- config filename
    "errors":      []string  -- validation errors (omitted when valid)
    "warnings":    []string  -- non-fatal warnings, e.g. self-detaching commands (omitted when none)
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  config is valid
  1  config is invalid, or error (file not found, cannot parse path)

```
eos api validate <path> [flags]
```

### Examples

```
  eos api validate ./path/to/project
  eos api validate ./path/to/project/service.yaml
  eos api validate ./path/to/project | jq .valid
```

### Options

```
  -h, --help   help for validate
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

