package cmd

import (
	"fmt"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	var forceQuit bool

	cmd := &cobra.Command{
		Use:   "stop <service-name>",
		Short: "Stop all processes for a service",
		Long: `Stops all the processes for a registered service.

This persists across a daemon restart, reboot, or "eos system update": the
service stays down until you bring it back with "eos run".`,
		Example: `  eos stop cms              # graceful stop with configurable grace period
  eos stop cms --force      # immediate kill`,
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		SilenceErrors:     true,
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()
			cfg := getConfig()

			stopCmdPrintStarting(cmd, serviceName, forceQuit)

			if err := stopCmdEnsureRegistered(cmd, mgr, serviceName); err != nil {
				return err
			}

			// Persist the stop as this service's desired boot state, regardless of
			// whether a process was actually still running to kill below: the
			// operator's intent is "don't bring this back", and bootPersistedServices
			// reads this flag to skip it on the next daemon start/reboot (issue #172).
			if err := mgr.SetServiceEnabled(cmd.Context(), serviceName, false); err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("persisting stopped state: %v", err))
				return helpers.ErrCommandFailed
			}

			if forceQuit {
				forceStopService(cmd, serviceName, mgr)
				return nil
			}

			stopResult, err := mgr.StopService(cmd.Context(), serviceName, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", err))
				return helpers.ErrCommandFailed
			}

			return stopCmdHandleResult(cmd, serviceName, mgr, stopResult)
		}}

	cmd.Flags().BoolVar(&forceQuit, "force", false, "force quit service immediately")

	return cmd
}

func stopCmdPrintStarting(cmd *cobra.Command, serviceName string, forceQuit bool) {
	if forceQuit {
		cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "forcefully stopping", ui.TextBold.Render(serviceName))
		return
	}
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "stopping", ui.TextBold.Render(serviceName))
}

func stopCmdEnsureRegistered(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) error {
	exists, err := mgr.IsServiceRegistered(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
		return helpers.ErrCommandFailed
	}
	if !exists {
		cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
		cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos add <path>"), ui.TextMuted.Render("to register it"))
		return helpers.ErrCommandFailed
	}
	return nil
}

func stopCmdHandleResult(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager, stopResult manager.StopServiceResult) error {
	countStopped := len(stopResult.Stopped)
	countError := len(stopResult.Errored)
	countStaleData := len(stopResult.StaleData)

	if countStopped == 0 && countError == 0 {
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), "no running processes found")
		cleanupServiceInstance(cmd, serviceName, mgr)
		return nil
	}

	stopCmdPrintStoppedCount(cmd, countStopped)
	stopCmdPrintStaleDataWarning(cmd, countStaleData)

	if countError == 0 {
		cleanupServiceInstance(cmd, serviceName, mgr)
		return nil
	}

	return stopCmdConfirmForceQuit(cmd, serviceName, mgr, stopResult.Errored)
}

func stopCmdPrintStoppedCount(cmd *cobra.Command, countStopped int) {
	if countStopped == 1 {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "stopped 1 process")
	} else if countStopped > 1 {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), fmt.Sprintf("stopped %d processes", countStopped))
	}
}

func stopCmdPrintStaleDataWarning(cmd *cobra.Command, countStaleData int) {
	if countStaleData > 0 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelWarning.Render("warning"),
			fmt.Sprintf("failed to update history for %d process(es) - data may be stale", countStaleData))
	}
}

func stopCmdConfirmForceQuit(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager, errored map[int]string) error {
	cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), "failed to gracefully stop", ui.TextBold.Render(serviceName))
	for erroredPGID, erroredMsg := range errored {
		cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextBold.Render(fmt.Sprintf("PGID %d:", erroredPGID)), erroredMsg)
	}

	confirmed := helpers.PromptConfirm(cmd, "force quit? (y/n):")
	if !confirmed {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "force quit aborted")
		return helpers.ErrCommandFailed
	}
	forceStopService(cmd, serviceName, mgr)
	return nil
}

func forceStopService(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager) {
	forceStopResult, err := mgr.ForceStopService(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("force stopping service: %v", err))
		return
	}

	countStopped := len(forceStopResult.Stopped)
	countStaleData := len(forceStopResult.StaleData)

	switch {
	case countStopped == 1:
		cmd.Printf(fmtLabelMsg, ui.LabelSuccess.Render("success"), "force stopped 1 process")
	case countStopped > 1:
		cmd.Printf(fmtLabelMsg, ui.LabelSuccess.Render("success"), fmt.Sprintf("force stopped %d processes", countStopped))
	default:
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), "force stopped no processes")
	}

	if countStaleData > 0 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelWarning.Render("warning"),
			fmt.Sprintf("failed to update history for %d process(es) - data may be stale", countStaleData))
	}

	if len(forceStopResult.Errored) > 0 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "failed to force stop service, manual action required")
		for erroredPGID, errored := range forceStopResult.Errored {
			cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextBold.Render(fmt.Sprintf("PGID %d:", erroredPGID)), errored)
		}
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("kill -9 <pgid>") + ui.TextMuted.Render(" to use a PGID listed above for manual kill") + "\n")
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)) + ui.TextMuted.Render(" to view service info") + "\n")
		return
	}

	cleanupServiceInstance(cmd, serviceName, mgr)
}

func cleanupServiceInstance(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager) {
	removed, err := mgr.RemoveServiceInstance(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("cleaning up service instance: %v", err))
		return
	}

	if !removed {
		cmd.Printf(fmtLabelMsgLn, ui.LabelWarning.Render("warning"), "no service instance removed")
		return
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelSuccess.Render("success"), "service instance cleaned up")
}
