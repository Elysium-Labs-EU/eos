## eos init

Generate a service.yaml for a project

### Synopsis

Interactively generate a service.yaml in the target directory. Detects runtime from project files to prefill defaults.

```
eos init [dir] [flags]
```

### Examples

```
  eos init              # generate service.yaml in current directory
  eos init ./myproject  # generate in a specific directory
```

### Options

```
      --force   overwrite existing service.yaml without prompting
  -h, --help    help for init
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

