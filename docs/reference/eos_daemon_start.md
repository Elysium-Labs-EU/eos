## eos daemon start

Start the daemon process

### Synopsis

Launch the deployment daemon.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).
If an OpenRC init script is installed, delegates to "rc-service eos start" (requires root).

Otherwise, starts the daemon detached in the background by default; control returns once the PID file is written (timeout: 5s). --detach (-d) is accepted for backward compatibility but is now a no-op. Pass --foreground (-f) to run in the foreground and stream output to the console instead — Ctrl-C will then stop the daemon.

```
eos daemon start [flags]
```

### Options

```
  -d, --detach       run daemon in background (default; kept for backward compatibility)
  -f, --foreground   run daemon in foreground and stream output (Ctrl-C stops it)
  -h, --help         help for start
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos daemon](eos_daemon.md)	 - Manage the deployment daemon

