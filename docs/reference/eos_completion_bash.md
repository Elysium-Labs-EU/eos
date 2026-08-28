## eos completion bash

Print bash completion script to stdout

### Synopsis

Print the bash completion script to stdout.

Install system-wide (requires sudo):
  sudo eos completion bash > /etc/bash_completion.d/eos

Install for current user (no sudo):
  eos completion bash > ~/.local/share/bash-completion/completions/eos

```
eos completion bash [flags]
```

### Options

```
  -h, --help   help for bash
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos completion](eos_completion.md)	 - Set up shell tab completion

