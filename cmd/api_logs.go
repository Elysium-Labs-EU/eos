package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiLogsResult struct {
	Name    string   `json:"name"`
	LogPath string   `json:"log_path"`
	Lines   []string `json:"lines"`
}

func newAPILogsCmd(getManager func() manager.ServiceManager) *cobra.Command {
	var lines int
	var errorLog bool

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Return service logs as JSON",
		Long: `Return the last N lines of a service log as a JSON array.

Output schema (stdout, JSON):
  {
    "name":     string    -- service name
    "log_path": string    -- absolute path to the log file
    "lines":    []string  -- log lines, oldest first
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

Note: --follow is not supported in the API version; use the log_path to tail directly.`,
		Example: `  eos api logs myservice
  eos api logs myservice --lines 50
  eos api logs myservice --error
  eos api logs myservice | jq '.lines[-1]'`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service: %w", err))
			}
			if !exists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q is not registered", serviceName))
			}

			processHistoryEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting process history: %w", err))
			}
			if processHistoryEntry == nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q has never been started", serviceName))
			}

			if lines < 0 || lines > 10000 {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("lines must be between 0 and 10000"))
			}

			logPath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting log file path: %w", err))
			}

			tailedLines, err := tailLogLines(*logPath, lines)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("reading log file: %w", err))
			}

			return helpers.WriteJSON(cmd, apiLogsResult{
				Name:    serviceName,
				LogPath: *logPath,
				Lines:   tailedLines,
			})
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "number of lines to return")
	cmd.Flags().BoolVar(&errorLog, "error", false, "return error log instead of output log")
	return cmd
}

func tailLogLines(path string, n int) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}
