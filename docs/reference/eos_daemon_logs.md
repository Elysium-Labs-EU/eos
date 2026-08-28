## eos daemon logs

View daemon log output

### Synopsis

Display or stream the daemon's log file. Defaults to the last 300 lines. Use --follow to tail in real time, --lines to control history depth. Accepts values between 0 and 10,000. Exit with Ctrl+C.

```
eos daemon logs [flags]
```

### Options

```
      --follow      follow log output
  -h, --help        help for logs
      --lines int   number of lines to display (default 300)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos daemon](eos_daemon.md)	 - Manage the deployment daemon

