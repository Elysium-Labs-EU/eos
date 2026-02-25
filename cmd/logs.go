package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"eos/internal/manager"
	"eos/internal/ui"
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

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
				return
			}
			if !exists {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos add <path>"), ui.TextMuted.Render("to register it"))
				return
			}

			processHistoryEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting process history: %v", err))
				return
			}
			if processHistoryEntry == nil {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "has never been started")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos start %s", serviceName)), ui.TextMuted.Render("to start it"))
				return
			}

			selectedLogFilepath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file path: %v", err))
				return
			}

			if lines < 0 || lines > 10000 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "line count must be between 0 and 10000")
				return
			}

			tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, *selectedLogFilepath)

			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "streaming logs for", ui.TextBold.Render(serviceName))

			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", tailArgs...)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			err = tailLogCommand.Run()

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("log command failed: %v", err))
			}
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "Number of lines to display")
	cmd.Flags().BoolVar(&errorLog, "error", false, "Show error logs instead of output logs")
	cmd.Flags().BoolVar(&follow, "follow", true, "Follow log output (disable for scripts/testing)")

	return cmd
}
