## eos reload

Zero-downtime reload of a service

### Synopsis

Reload a running service without dropping connections.

eos starts a fresh instance alongside the running one, waits for the new
instance to pass its health check, and only then drains the old instance. If the
new instance never becomes healthy the old one keeps serving, so a broken deploy
is a no-op rather than an outage.

Unlike restart (stop-then-start, which drops the listening socket in between),
reload overlaps the two instances. That overlap only works if the service shares
its listening socket: the service MUST bind its port with SO_REUSEPORT so both
the old and new instance can listen on the same address at once, and it must bind
promptly on startup so the new instance is accepting before the old one is
drained. eos does not own the socket or proxy traffic; it only sequences the
cutover. A service that binds without SO_REUSEPORT will fail to start its second
instance (address already in use) and the reload will abort with the old
instance untouched.

```
eos reload <service-name> [flags]
```

### Examples

```
  eos reload cms    # start a new instance, health-check it, then drain the old one
```

### Options

```
  -h, --help   help for reload
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

