## eos config init

Scaffold ~/.eos/config.yaml with default values

### Synopsis

Write a fully commented ~/.eos/config.yaml showing every available field at its default value. Non-interactive; edit the file afterward to change settings.

```
eos config init [flags]
```

### Examples

```
  eos config init
  eos config init --force  # overwrite an existing file
```

### Options

```
      --force   overwrite an existing config.yaml without prompting
  -h, --help    help for init
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos config](eos_config.md)	 - Inspect and scaffold the eos daemon configuration

