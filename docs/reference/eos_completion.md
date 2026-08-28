## eos completion

Set up shell tab completion

### Synopsis

Set up tab completion so that eos commands and service names complete on <Tab>.

Running without a subcommand detects your shell and prompts to install.
To print the script to stdout instead (for manual setup or scripting), pass the shell name:

  eos completion bash
  eos completion zsh
  eos completion fish

```
eos completion [flags]
```

### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool
* [eos completion bash](eos_completion_bash.md)	 - Print bash completion script to stdout
* [eos completion fish](eos_completion_fish.md)	 - Print fish completion script to stdout
* [eos completion zsh](eos_completion_zsh.md)	 - Print zsh completion script to stdout

