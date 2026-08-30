## eos api update

Update a service's directory path; always outputs JSON

### Synopsis

Update the directory path for an existing registered service.

Output schema (stdout, JSON):
  {
    "name":        string  -- service name
    "path":        string  -- new absolute directory path
    "config_file": string  -- config filename in the new directory
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api update <service-name> <new-path> [flags]
```

### Examples

```
  eos api update myservice /new/path/to/project
  eos api update myservice /new/path | jq .path
```

### Options

```
  -h, --help   help for update
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api](eos_api.md)	 - Machine-readable JSON interface

