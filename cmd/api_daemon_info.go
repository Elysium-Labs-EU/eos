package cmd

import (
	"errors"
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/process"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonInfoResult struct {
	Running          *bool  `json:"running,omitempty"`
	Pid              *int   `json:"pid,omitempty"`
	UserUnit         *bool  `json:"user_unit,omitempty"`
	UserAgent        *bool  `json:"user_agent,omitempty"`
	Mode             string `json:"mode"`
	PIDFile          string `json:"pid_file,omitempty"`
	SocketPath       string `json:"socket_path,omitempty"`
	SocketTimeout    string `json:"socket_timeout,omitempty"`
	LogDir           string `json:"log_dir,omitempty"`
	LogFileName      string `json:"log_file_name,omitempty"`
	LogMaxFiles      int    `json:"log_max_files,omitempty"`
	LogFileSizeLimit int64  `json:"log_file_size_limit,omitempty"`
}

func newAPIDaemonInfoCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Return daemon status and configuration as JSON",
		Long: `Return the daemon's supervisor mode and configuration. For a standalone daemon, includes live running status, PID, socket, and log paths. For systemd- or launchd-managed daemons, only the supervisor mode and unit scope are returned — use the native tool (systemctl/launchctl) for runtime state.

Output schema (stdout, JSON):
  {
    "mode":                string        -- "standalone", "systemd", or "launchd"
    "running":             bool|omitted  -- standalone only
    "pid":                 int|omitted   -- standalone only, present when running
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
  1  error`,
		Example: `  eos api daemon info
  eos api daemon info | jq '.running'`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, _, err := getConfig()
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting config: %w", err))
			}

			switch {
			case cfg.Daemon.Standalone != nil:
				status, statusErr := process.StatusStandaloneDaemon(cfg.Daemon.Standalone)
				if statusErr != nil {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("checking daemon status: %w", statusErr))
				}
				return helpers.WriteJSON(cmd, apiDaemonInfoResult{
					Mode:             "standalone",
					Running:          &status.Running,
					Pid:              status.Pid,
					PIDFile:          cfg.Daemon.Standalone.PIDFile,
					SocketPath:       cfg.Daemon.Standalone.SocketPath,
					SocketTimeout:    cfg.Daemon.Standalone.SocketTimeout.String(),
					LogDir:           cfg.Daemon.Standalone.Log.LogDir,
					LogFileName:      cfg.Daemon.Standalone.Log.LogFileName,
					LogMaxFiles:      cfg.Daemon.Standalone.Log.LogMaxFiles,
					LogFileSizeLimit: cfg.Daemon.Standalone.Log.LogFileSizeLimit,
				})
			case cfg.Daemon.Systemd != nil:
				return helpers.WriteJSON(cmd, apiDaemonInfoResult{Mode: "systemd", UserUnit: &cfg.Daemon.Systemd.UserUnit})
			case cfg.Daemon.Launchd != nil:
				return helpers.WriteJSON(cmd, apiDaemonInfoResult{Mode: "launchd", UserAgent: &cfg.Daemon.Launchd.UserAgent})
			default:
				return helpers.WriteJSONErr(cmd, errors.New("invalid daemon config: standalone, systemd, and launchd are all nil"))
			}
		},
	}
}
