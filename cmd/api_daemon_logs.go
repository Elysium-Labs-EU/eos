package cmd

import (
	"fmt"
	"path/filepath"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiDaemonLogsResult struct {
	LogPath string   `json:"log_path"`
	Lines   []string `json:"lines"`
}

func newAPIDaemonCmd(getConfig func() (string, *config.SystemConfig, error)) *cobra.Command {
	daemonCmd := &cobra.Command{
		Use:           "daemon",
		Short:         "Machine-readable daemon interface",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	daemonCmd.AddCommand(newAPIDaemonLogsCmd(getConfig))
	return daemonCmd
}

func newAPIDaemonLogsCmd(getConfig func() (string, *config.SystemConfig, error)) *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Return daemon logs as JSON",
		Long: `Return the last N lines of the daemon log as a JSON array.

Standalone daemon only. For systemd-managed daemons, use journalctl directly:
  journalctl -u eos -n 300

Output schema (stdout, JSON):
  {
    "log_path": string    -- absolute path to the daemon log file
    "lines":    []string  -- log lines, oldest first
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api daemon logs
  eos api daemon logs --lines 50
  eos api daemon logs | jq '.lines[-1]'`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			baseDir, cfg, err := getConfig()
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting config: %w", err))
			}

			if cfg.Daemon.Standalone == nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("daemon is managed by systemd; use 'journalctl -u eos' to access logs"))
			}

			if lines < 0 || lines > 10000 {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("lines must be between 0 and 10000"))
			}

			logPath := filepath.Join(manager.CreateLogDirPath(baseDir), cfg.Daemon.Standalone.Log.LogFileName)

			tailedLines, err := tailLogLines(logPath, lines)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("reading log file: %w", err))
			}

			return helpers.WriteJSON(cmd, apiDaemonLogsResult{
				LogPath: logPath,
				Lines:   tailedLines,
			})
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "number of lines to return")
	return cmd
}
