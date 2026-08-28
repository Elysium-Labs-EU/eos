## eos completion zsh

Print zsh completion script to stdout

### Synopsis

Print the zsh completion script to stdout.

Install:
  eos completion zsh > "${fpath[1]}/_eos"

Then reload: exec $SHELL

```
eos completion zsh [flags]
```

### Options

```
  -h, --help   help for zsh
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos completion](eos_completion.md)	 - Set up shell tab completion

