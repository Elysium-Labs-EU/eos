package cmd

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"eos/internal/config"
	"eos/internal/manager"
	"eos/internal/ui"
)

func newStopCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	var forceQuit bool

	cmd := &cobra.Command{
		Use:   "stop <service-name>",
		Short: "Stop all processes for a service",
		Long:  `Stops all the processes for a registered service.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()
			cfg := getConfig()

			if forceQuit {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "forcefully stopping", ui.TextBold.Render(serviceName))
			} else {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "stopping", ui.TextBold.Render(serviceName))
			}

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

			if forceQuit {
				forceStopService(cmd, serviceName, mgr)
				return
			}

			stopResult, err := mgr.StopService(serviceName, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", err))
				return
			}

			countStopped := len(stopResult.Stopped)
			countError := len(stopResult.Errored)
			countStaleData := len(stopResult.StaleData)

			if countStopped == 0 && countError == 0 {
				cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "no running processes found")
				cleanupServiceInstance(cmd, serviceName, mgr)
				return
			}

			if countStopped == 1 {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "stopped 1 process")
			} else if countStopped > 1 {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("stopped %d processes", countStopped))
			}

			if countStaleData > 0 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelWarning.Render("warning"),
					fmt.Sprintf("failed to update history for %d process(es) — data may be stale", countStaleData))
			}

			if countError == 0 {
				cleanupServiceInstance(cmd, serviceName, mgr)
				return
			}

			cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), "failed to gracefully stop", ui.TextBold.Render(serviceName))
			for erroredPid, errored := range stopResult.Errored {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(fmt.Sprintf("PID %d:", erroredPid)), errored)
			}
			cmd.Printf("  %s ", ui.TextMuted.Render("force quit? (y/n):"))

			reader := bufio.NewReader(cmd.InOrStdin())
			response, err := reader.ReadString('\n')

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading input: %v", err))
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))

			if response != "y" && response != "yes" {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "force quit aborted")
				return
			}
			forceStopService(cmd, serviceName, mgr)
		}}

	cmd.Flags().BoolVar(&forceQuit, "force", false, "force quit service immediately")

	return cmd
}

func forceStopService(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager) {
	forceStopResult, err := mgr.ForceStopService(serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("force stopping service: %v", err))
		return
	}

	countStopped := len(forceStopResult.Stopped)
	countStaleData := len(forceStopResult.StaleData)

	if countStopped == 1 {
		cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), "force stopped 1 process")
	} else if countStopped > 1 {
		cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), fmt.Sprintf("force stopped %d processes", countStopped))
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "force stopped no processes")
	}

	if countStaleData > 0 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelWarning.Render("warning"),
			fmt.Sprintf("failed to update history for %d process(es) — data may be stale", countStaleData))
	}

	if len(forceStopResult.Errored) > 0 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "failed to force stop service, manual action required")
		for erroredPid, errored := range forceStopResult.Errored {
			cmd.PrintErrf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(fmt.Sprintf("PID %d:", erroredPid)), errored)
		}
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)) + ui.TextMuted.Render(" to view info on service") + "\n")
		return
	}

	cleanupServiceInstance(cmd, serviceName, mgr)
}

func cleanupServiceInstance(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager) {
	removed, err := mgr.RemoveServiceInstance(serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleaning up service instance: %v", err))
		return
	}

	if !removed {
		cmd.Printf("%s %s\n", ui.LabelWarning.Render("warning"), "no service instance removed")
		return
	}

	cmd.Printf("%s %s\n", ui.LabelSuccess.Render("success"), "service instance cleaned up")
}
