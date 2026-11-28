## eos add

Register a service from a directory

### Synopsis

Register a service by providing the path to a directory containing a service configuration.

Registering a service also enables it: it auto-starts on every future daemon
boot (reboot, "eos daemon start", "eos system update") until it's stopped by
hand with "eos stop".

```
eos add <path> [flags]
```

### Examples

```
  eos add ./path/to/project            # find service.yaml automatically in the directory
 eos add ./path/to/project/service.yaml  # point directly to the config file
```

### Options

```
  -h, --help   help for add
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

