## eos stop

Stop all processes for a service

### Synopsis

Stops all the processes for a registered service.

This persists across a daemon restart, reboot, or "eos system update": the
service stays down until you bring it back with "eos run".

```
eos stop <service-name> [flags]
```

### Examples

```
  eos stop cms              # graceful stop with configurable grace period
  eos stop cms --force      # immediate kill
```

### Options

```
      --force   force quit service immediately
  -h, --help    help for stop
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

