## eos daemon info

Show daemon status and configuration

### Synopsis

Display daemon status and configuration. For systemd-managed daemons, shows configuration only (use 'systemctl status eos.service' for runtime state). For standalone daemons, shows whether the process is running, its PID, socket path, log directory, log file name, max file count, and file size limit. Reports clearly if the daemon is stopped or not found.

Pass --all (root only) to enumerate every user's standalone daemon on the host instead of just the invoking user's, flagging any still running against a since-replaced binary.

```
eos daemon info [flags]
```

### Options

```
      --all    list every user's standalone daemon on this host (root only)
  -h, --help   help for info
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos daemon](eos_daemon.md)	 - Manage the deployment daemon

