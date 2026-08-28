## eos remove

Remove a service from the registry

### Synopsis

Unregisters a service and removes its instance if one exists. Does not stop the service process if it is currently running.

```
eos remove <service-name> [flags]
```

### Examples

```
  eos remove cms    # unregisters cms; does not stop a running process
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

* [eos](eos.md)	 - A service supervisor CLI tool

