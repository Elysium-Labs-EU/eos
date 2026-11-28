## eos daemon remove

Remove a stopped daemon

### Synopsis

Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.

```
eos daemon remove [flags]
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

* [eos daemon](eos_daemon.md)	 - Manage the deployment daemon

