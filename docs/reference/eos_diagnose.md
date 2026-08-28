## eos diagnose

Bundle a privacy-safe diagnostic archive for bug reports

### Synopsis

Collect version, daemon, and per-service status plus recent logs into a
single tar.gz suitable for attaching to a public GitHub issue.

The bundle uses an allowlist, not a secret-pattern scrubber, as its primary
defense: only positively-approved fields (version, service names, status
enums, timestamps, PID/uptime/restart counts, and log lines run through a
secondary regex scrub) are ever written. It never includes raw env vars,
service.yaml bodies, absolute paths, or the raw hostname — the hostname is
replaced with a truncated hash so a maintainer can recognize "same box,
second report" without learning any identifying string.

Every collection step (version, daemon status, each service's status, log
tails) is recorded independently in the bundle's manifest.json as captured or
failed; a daemon that's down, one bad service.yaml, or one unreadable log
file never prevents a bundle from being produced. Only a failure to write
the output file itself aborts the command.

The manager used to read this state is acquired the same way --no-daemon
does — the state DB is opened directly off disk, never through the daemon
socket — so running this command never starts the very daemon whose failure
it might be diagnosing.

daemon-env.json and service-env.json are always collected: the daemon
process's own environment and each running service's resolved PATH, read
from the live processes themselves rather than inferred from config. Both
are allowlist-redacted (PATH, HOME, USER, SHELL, LANG, PWD, and the
variables systemd sets) -- a name outside that allowlist is listed with its
value withheld, never dropped, so the bundle still shows the shape of the
environment without risking a leaked secret. This is different from
--include-env, which dumps each service's configured env_file unredacted.

--include-env writes a raw, unredacted dump of each service's resolved
env_file. It is never included by default: do not attach that output to a
public issue.

```
eos diagnose [flags]
```

### Examples

```
  eos diagnose
  eos diagnose --since 30m --lines 2000
  eos diagnose --output /tmp/bug-report.tar.gz
  eos diagnose --no-service-logs
```

### Options

```
  -h, --help              help for diagnose
      --include-env       include raw, unredacted env_file dumps — do not attach this to a public issue
      --lines int         hard cap on lines per file (default 1000)
      --no-service-logs   skip per-service logs (they can only be scrubbed, not allowlisted)
      --output string     output tar.gz path (default ./eos-diagnose-<timestamp>.tar.gz)
      --since duration    time window for daemon.log (default 10m0s)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos](eos.md)	 - A service supervisor CLI tool

