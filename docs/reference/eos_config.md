## eos config

Inspect and scaffold the eos daemon configuration

### Synopsis

View, scaffold, and validate ~/.eos/config.yaml — the daemon-wide settings for
the log sink registry, telemetry export, health thresholds, and log rotation.

This is distinct from service.yaml, which configures one registered service (see "eos init").

### Options

```
  -h, --help   help for config
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool
* [eos config init](eos_config_init.md)	 - Scaffold ~/.eos/config.yaml with default values
* [eos config show](eos_config_show.md)	 - Print the effective config.yaml
* [eos config validate](eos_config_validate.md)	 - Validate ~/.eos/config.yaml

