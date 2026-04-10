package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newLogsCmd(getManager func() manager.ServiceManager) *cobra.Command {
	var lines int
	var errorLog bool
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View logs for a registered service",
		Long:  `Stream or display logs for a registered service. Shows output logs by default; use --error for error logs, --lines to control history depth, and --follow to tail in real time.`,
		Example: `  eos logs cms                   # last 300 lines of stdout log
  eos logs cms --lines 100      # last 100 lines
  eos logs cms --follow         # stream live output
  eos logs cms --error          # error log instead of output log`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
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

			if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting process history: %v", err))
				return
			}
			if processHistoryEntry == nil {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "has never been started")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos start %s", serviceName)), ui.TextMuted.Render("to start it"))
				return
			}

			if lines < 0 || lines > 10000 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "line count must be between 0 and 10000")
				return
			}

			logPath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file path: %v", err))
				return
			}

			tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, *logPath)

			if follow {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "streaming logs for", ui.TextBold.Render(serviceName))
			} else {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "showing logs for", ui.TextBold.Render(serviceName))
			}

			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", tailArgs...)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			err = tailLogCommand.Run()

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading log file: %v", err))
			}
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display")
	cmd.Flags().BoolVar(&errorLog, "error", false, "show error logs instead of output logs")
	cmd.Flags().BoolVar(&follow, "follow", false, "follow log output")

	return cmd
}
