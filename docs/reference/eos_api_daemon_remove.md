## eos api daemon remove

Remove a stopped daemon; always outputs JSON

### Synopsis

Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.

Output schema (stdout, JSON):
  {
    "removed": bool  -- true on success
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api daemon remove [flags]
```

### Examples

```
  eos api daemon remove
  eos api daemon remove | jq .removed
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

* [eos api daemon](eos_api_daemon.md)	 - Machine-readable daemon interface

