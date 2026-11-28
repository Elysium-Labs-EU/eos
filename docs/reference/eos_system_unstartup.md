## eos system unstartup

Disable eos from starting automatically on boot

### Synopsis

Remove the systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and disable it from running on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): removes the system unit / LaunchDaemon / OpenRC init script.
  - Run as a regular user: removes the user unit / LaunchAgent.

On OpenRC, removes the system-wide init script at /etc/init.d/eos and requires root.

```
eos system unstartup [flags]
```

### Examples

```
  sudo eos system unstartup  # remove system unit
       eos system unstartup  # remove user unit (systemd/launchd only)
       eos system unstartup --yes  # skip confirmation (non-interactive)
```

### Options

```
  -h, --help   help for unstartup
  -y, --yes    skip all confirmation prompts (non-interactive mode)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos system](eos_system.md)	 - Manage the eos system settings

