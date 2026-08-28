## eos logs

View logs for a registered service

### Synopsis

Stream or display logs for a registered service. Shows both stdout and stderr logs interleaved by default.
Use --output for stdout only, --error for stderr only, --lines to control history depth, and --follow to tail in real time.

In combined mode --lines applies per stream, so up to 2x lines may be shown. Each line is prefixed with a dim "out" or bold "err" label to identify the source stream.

```
eos logs <service-name> [flags]
```

### Examples

```
  eos logs cms                   # last 300 lines from both streams combined
  eos logs cms --lines 100      # last 100 lines per stream combined
  eos logs cms --follow         # stream live output from both streams
  eos logs cms --error          # error stream only
  eos logs cms --output         # stdout stream only
```

### Options

```
      --error       show error stream only
      --follow      follow log output
  -h, --help        help for logs
      --lines int   number of lines to display (per stream in combined mode) (default 300)
      --output      show stdout stream only
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

