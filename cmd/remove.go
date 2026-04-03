package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"eos/cmd/helpers"
	"eos/internal/manager"
	"eos/internal/ui"
)

// TODO: Add interactive check for running process, giving user option to continue or not.
func newRemoveCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:               "remove <service-name>",
		Short:             "Remove a service from the registry",
		Long:              `Unregisters a service and removes its instance if one exists. Does not stop the service process if it is currently running.`,
		Example:           `  eos remove cms    # unregisters cms; does not stop a running process`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
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
			// NOTE: We check here on both string and error type. String because of daemon serialization.
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) && !strings.Contains(err.Error(), manager.ErrServiceNotRunning.Error()) {
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
