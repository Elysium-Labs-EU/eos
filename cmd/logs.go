package cmd

import (
	"eos/internal/manager"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
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

			_, err = mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil {
				cmd.Printf("Service '%s' has never ran\n", serviceName)
				return
			}

			selectedLogFilepath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)

			if err != nil {
				cmd.Printf("An error occured during getting the log file, got:\n%v", err)
				return
			}

			tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, *selectedLogFilepath)

			tailLogCommand := exec.Command("tail", tailArgs...)
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
