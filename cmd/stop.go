package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"eos/internal/manager"
	"eos/internal/ui"
)

func newStopCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <service-name>",
		Short: "Stop all processes for a service",
		Long:  `Stops all the processes for a registered service.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "stopping", ui.TextBold.Render(serviceName))

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

			stopResult, err := mgr.StopService(serviceName)

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", err))
				return
			}

			if len(stopResult.Stopped) == 0 && len(stopResult.Failed) == 0 {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "no running processes found for", ui.TextBold.Render(serviceName))
				cleanupServiceInstance(cmd, serviceName, mgr)
				return
			}

			if len(stopResult.Stopped) > 0 && len(stopResult.Failed) == 0 {
				cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("stopped (%d processes)", len(stopResult.Stopped)))
				cleanupServiceInstance(cmd, serviceName, mgr)
				return
			}

			if len(stopResult.Stopped) > 0 {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("stopped %d processes of %s", len(stopResult.Stopped), ui.TextBold.Render(serviceName)))
			}

			if len(stopResult.Failed) > 0 {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), "failed to gracefully stop", ui.TextBold.Render(serviceName))
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

				forceStopResult, err := mgr.ForceStopService(serviceName)
				if err != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("force stopping service: %v", err))
					return
				}

				if len(forceStopResult.Stopped) > 0 {
					cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), fmt.Sprintf("force stopped %d processes", len(forceStopResult.Stopped)))
				}

				if len(forceStopResult.Failed) > 0 {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "failed to force stop service, manual action required")
					return
				}

				cleanupServiceInstance(cmd, serviceName, mgr)
				return
			}

			cleanupServiceInstance(cmd, serviceName, mgr)
		}}
}

func cleanupServiceInstance(cmd *cobra.Command, serviceName string, mgr manager.ServiceManager) {
	removed, err := mgr.RemoveServiceInstance(serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleaning up service instance: %v", err))
		return
	}

	if !removed {
		cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "service was not running")
		return
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "service instance cleaned up")
}
