## eos run

Start or restart a service

### Synopsis

Start a service by name or from a service file.

		If the service is already running it will be restarted, unless --once is set.

		Talking to a live eos daemon, this returns as soon as the service starts
		and the daemon supervises it from then on. Without a daemon (--no-daemon,
		or a supervised install whose unit is currently down), there is nothing
		else to supervise the service: this command blocks in the foreground and
		does so itself, until interrupted with Ctrl-C (SIGINT/SIGTERM), at which
		point it stops the service gracefully before exiting. Run it in the
		background (eos run myservice &) to script it in local mode.

		Examples:
		eos run myservice              start or restart a registered service
		eos run -f ./myservice.yaml    register and start from a service file
		eos run --once myservice       start only if not already running

```
eos run [flags] [name]
```

### Options

```
  -f, --file string   use file to run the service
  -h, --help          help for run
      --once          do nothing if service is already running/starting
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

