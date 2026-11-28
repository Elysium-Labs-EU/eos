## eos status

Show the status of all services

### Synopsis

Display the current status of all configured services including their running state, process IDs, and health information.

```
eos status [flags]
```

### Examples

```
  eos status
  eos status --watch
  eos status --watch --interval 5
```

### Options

```
  -h, --help           help for status
  -i, --interval int   refresh interval in seconds (only with --watch) (default 2)
  -w, --watch          watch mode: refresh status periodically
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

