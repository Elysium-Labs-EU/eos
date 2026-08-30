## eos env

Inspect or edit a service's environment variables

### Synopsis

Prints the resolved environment variables for a registered service, sourced
from its env_file. Reads directly from disk, so it reflects the current
env_file contents without requiring the service to be running or restarted.

Use "set KEY=VALUE" to add or update a variable in the service's env_file, or
"unset KEY" to remove one. Both require the service to have env_file configured.

```
eos env <service> [set KEY=VALUE|unset KEY] [flags]
```

### Examples

```
  eos env cms                     # list resolved env vars
  eos env cms set DEBUG=true      # write DEBUG=true to env_file
  eos env cms unset DEBUG         # remove DEBUG from env_file
```

### Options

```
  -h, --help   help for env
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

