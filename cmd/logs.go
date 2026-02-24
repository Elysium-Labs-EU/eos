package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"eos/internal/manager"
)

func newLogsCmd(getManager func() manager.ServiceManager) *cobra.Command {
	var lines int
	var errorLog bool
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Shows service logs",
		Long:  "Shows the logs for a specific service",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			cmd.Printf("Checking the logs for %s \n", serviceName)

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.Printf("Error checking service: %v\n", err)
				return
			}
			if !exists {
				cmd.Printf("Service '%s' is not registered\n", serviceName)
				return
			}

			processHistoryEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrNotFound) {
				cmd.Printf("Service unable to get recent process history entry, got: %v", err)
				return
			}
			if processHistoryEntry == nil {
				cmd.Printf("Service '%s' has never ran\n", serviceName)
				return
			}

			selectedLogFilepath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)

			if err != nil {
				cmd.Printf("An error occurred during getting the log file, got:\n%v", err)
				return
			}

			if lines < 0 || lines > 10000 {
				cmd.Printf("An invalid line count was used, should be between 0 and 10000")
				return
			}
			tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, *selectedLogFilepath)

			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", tailArgs...)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			err = tailLogCommand.Run()

			if err != nil {
				cmd.Printf("The log command failed, got:\n%v", err)
			}
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "Number of lines to display")
	cmd.Flags().BoolVar(&errorLog, "error", false, "Show error logs instead of output logs")
	cmd.Flags().BoolVar(&follow, "follow", true, "Follow log output (disable for scripts/testing)")

	return cmd
}
