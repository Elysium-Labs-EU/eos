## eos system uninstall

Remove eos from this system

### Synopsis

Stops all running services, removes the eos binary and configuration, and cleans up the install directory. Prompts for confirmation unless --yes is passed.

```
eos system uninstall [flags]
```

### Examples

```
  eos system uninstall        # interactive uninstall with confirmation prompt
  eos system uninstall --yes  # skip confirmation (non-interactive)
```

### Options

```
  -h, --help   help for uninstall
  -y, --yes    skip all confirmation prompts (non-interactive mode)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos system](eos_system.md)	 - Manage the eos system settings

