## eos system update

Apply new update if available

### Synopsis

Check GitHub for a newer eos release and optionally download and install it. Uses SHA256 checksum validation and backs up the current binary before replacing it.

```
eos system update [flags]
```

### Examples

```
  eos system update        # check and apply latest stable release
  eos system update --pre  # include pre-releases
```

### Options

```
  -h, --help   help for update
      --pre    includes pre-releases in update check
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos system](eos_system.md)	 - Manage the eos system settings

