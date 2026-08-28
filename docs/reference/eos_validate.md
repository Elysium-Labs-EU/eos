## eos validate

Validate a service configuration file

### Synopsis

Validate a service.yaml without registering it or requiring the daemon to run.

```
eos validate <path> [flags]
```

### Examples

```
  eos validate ./path/to/project            # find service.yaml automatically in the directory
  eos validate ./path/to/project/service.yaml  # point directly to the config file
```

### Options

```
  -h, --help   help for validate
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

