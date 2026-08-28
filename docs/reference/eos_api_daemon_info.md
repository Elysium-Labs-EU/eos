## eos api daemon info

Return daemon status and configuration as JSON

### Synopsis

Return the daemon's supervisor mode and configuration. For a standalone daemon, includes live running status, PID, socket, and log paths. For a systemd-managed daemon, includes live running status (via a base-dir-scoped socket probe, not the host-global "systemctl is-active") and PID when running. For a launchd-managed daemon, only the supervisor mode and unit scope are returned — use the native tool (launchctl) for runtime state.

Output schema (stdout, JSON):
  {
    "mode":                string        -- "standalone", "systemd", or "launchd"
    "running":             bool|omitted  -- standalone and systemd only
    "pid":                 int|omitted   -- standalone and systemd only, present when running
    "pid_file":            string|omitted-- standalone only
    "socket_path":         string|omitted-- standalone only
    "socket_timeout":      string|omitted-- standalone only
    "log_dir":             string|omitted-- standalone only
    "log_file_name":       string|omitted-- standalone only
    "log_max_files":       int|omitted   -- standalone only
    "log_file_size_limit": int|omitted   -- standalone only
    "user_unit":           bool|omitted  -- systemd only
    "user_agent":          bool|omitted  -- launchd only
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

```
eos api daemon info [flags]
```

### Examples

```
  eos api daemon info
  eos api daemon info | jq '.running'
```

### Options

```
  -h, --help   help for info
```

### Options inherited from parent commands

```
      --no-daemon   run in local mode without daemon
      --verbose     enable verbose debug logging
```

### SEE ALSO

* [eos api daemon](eos_api_daemon.md)	 - Machine-readable daemon interface

