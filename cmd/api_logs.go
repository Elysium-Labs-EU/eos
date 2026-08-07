package cmd

import (
	"context"
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

			if err := apiLogsEnsureServiceRegistered(cmd.Context(), mgr, serviceName); err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if err := apiLogsEnsureServiceStarted(cmd.Context(), mgr, serviceName); err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if err := apiLogsValidateLineCount(lines); err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			logPath, tailedLines, err := apiLogsFetchLines(cmd.Context(), mgr, serviceName, errorLog, lines)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			return helpers.WriteJSON(cmd, apiLogsResult{
				Name:    serviceName,
				LogPath: logPath,
				Lines:   tailedLines,
			})
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "number of lines to return")
	cmd.Flags().BoolVar(&errorLog, "error", false, "return error log instead of output log")
	return cmd
}

func apiLogsEnsureServiceRegistered(ctx context.Context, mgr manager.ServiceManager, serviceName string) error {
	exists, err := mgr.IsServiceRegistered(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("checking service: %w", err)
	}
	if !exists {
		return fmt.Errorf("service %q is not registered", serviceName)
	}
	return nil
}

func apiLogsEnsureServiceStarted(ctx context.Context, mgr manager.ServiceManager, serviceName string) error {
	processHistoryEntry, err := mgr.GetMostRecentProcessHistoryEntry(ctx, serviceName)
	if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
		return fmt.Errorf("getting process history: %w", err)
	}
	if processHistoryEntry == nil {
		return fmt.Errorf("service %q has never been started", serviceName)
	}
	return nil
}

func apiLogsValidateLineCount(lines int) error {
	if lines < 0 || lines > 10000 {
		return fmt.Errorf("lines must be between 0 and 10000")
	}
	return nil
}

func apiLogsFetchLines(ctx context.Context, mgr manager.ServiceManager, serviceName string, errorLog bool, lines int) (string, []string, error) {
	logPath, err := mgr.GetServiceLogFilePath(ctx, serviceName, errorLog)
	if err != nil {
		return "", nil, fmt.Errorf("getting log file path: %w", err)
	}

	tailedLines, err := tailLogLines(*logPath, lines)
	if err != nil {
		return "", nil, fmt.Errorf("reading log file: %w", err)
	}

	return *logPath, tailedLines, nil
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
