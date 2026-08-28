## eos system startup

Enable eos to start automatically on boot

### Synopsis

Install a systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and enable it to run on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): installs a system unit at /etc/systemd/system/eos.service, a LaunchDaemon at /Library/LaunchDaemons/org.elysiumlabs.eos.plist on macOS, or a system-wide OpenRC init script at /etc/init.d/eos — one per host, daemon runs as the invoking user.
  - Run as a regular user: installs a user unit at ~/.config/systemd/user/eos.service, or a LaunchAgent at ~/Library/LaunchAgents/org.elysiumlabs.eos.plist on macOS — each user gets their own, no root required.

A systemd user unit runs inside your personal --user systemd instance, which
by default only exists while you are logged in: close the SSH session or log
out of the desktop, and systemd tears down that instance along with every
unit running in it — including eos. A system unit has no such dependency; it
runs under the system-wide systemd instance, which starts at boot regardless
of who (if anyone) is logged in.

"linger" (loginctl enable-linger <username>) closes that gap by telling
systemd to keep your user instance running with no one logged in, the way a
system unit always does. This command offers to enable it right after
installing a user unit, and it's what most single-user servers (VPS,
homeserver) want — otherwise the service silently dies the moment you
disconnect. Skip it only if you deliberately want eos to run solely while
you're logged in (e.g. a desktop session). Installing as root/system unit
never needs linger, since it isn't tied to a login session in the first
place.

On OpenRC, installs a system-wide init script at /etc/init.d/eos and requires root — OpenRC has no per-user service scope.

Enabling this also revives every previously-registered service that wasn't
stopped by hand: the daemon starts every service in its catalog on boot,
skipping only those last stopped with "eos stop" — not just the eos daemon
itself.

```
eos system startup [flags]
```

### Examples

```
  sudo eos system startup  # system unit (root, one per host)
       eos system startup  # user unit (no root, per-user, systemd/launchd only)
       eos system startup --yes  # skip confirmation (non-interactive)
```

### Options

```
  -h, --help   help for startup
  -y, --yes    skip all confirmation prompts (non-interactive mode)
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos system](eos_system.md)	 - Manage the eos system settings

