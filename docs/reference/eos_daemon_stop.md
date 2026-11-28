## eos daemon stop

Stop the running daemon

### Synopsis

Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root); if managed by OpenRC, delegates to rc-service stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.

```
eos daemon stop [flags]
```

### Options

```
  -h, --help   help for stop
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos daemon](eos_daemon.md)	 - Manage the deployment daemon

