package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"eos/internal/manager"
	"eos/internal/ui"
)

func newRemoveCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <service-name>",
		Short: "Unregister a service",
		Long:  `Remove a service from the registry. This does not stop the service if it's running.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "this does not stop the service if it's running")

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

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service instance: %v", err))
				return
			}

			if serviceInstance != nil {
				removedInstance, removeInstanceErr := mgr.RemoveServiceInstance(serviceName)
				if removeInstanceErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing service instance: %v", removeInstanceErr))
					return
				}
				if !removedInstance {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "unable to remove service instance")
					return
				}
				cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "service instance removed")
			}

			removed, err := mgr.RemoveServiceCatalogEntry(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing service: %v", err))
				return
			}

			if !removed {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "could not be removed")
				return
			}

			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), "unregistered")
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos list"), ui.TextMuted.Render("→ view registered services"))
		}}
}
