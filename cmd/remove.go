package cmd

import (
	"errors"
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newRemoveCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:               "remove <service-name>",
		Short:             "Remove a service from the registry",
		Long:              `Unregisters a service and removes its instance if one exists. Does not stop the service process if it is currently running.`,
		Example:           `  eos remove cms    # unregisters cms; does not stop a running process`,
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		SilenceErrors:     true,
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			if err := removeCmdEnsureRegistered(cmd, mgr, serviceName); err != nil {
				return err
			}

			if err := removeCmdConfirmIfRunning(cmd, mgr, serviceName); err != nil {
				return err
			}

			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "this does not stop the service if it's running")

			if err := removeCmdRemoveInstance(cmd, mgr, serviceName); err != nil {
				return err
			}

			if err := removeCmdRemoveCatalogEntry(cmd, mgr, serviceName); err != nil {
				return err
			}

			removeCmdPrintSuccess(cmd, serviceName)
			return nil
		},
	}
}

func removeCmdEnsureRegistered(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) error {
	exists, err := mgr.IsServiceRegistered(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
		return helpers.ErrCommandFailed
	}

	if !exists {
		cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
		cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos add <path>"), ui.TextMuted.Render("to register it"))
		return helpers.ErrCommandFailed
	}

	return nil
}

func removeCmdConfirmIfRunning(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) error {
	history, err := mgr.GetMostRecentProcessHistoryEntry(cmd.Context(), serviceName)
	if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) && !errors.Is(err, manager.ErrProcessNotFound) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service state: %v", err))
		return helpers.ErrCommandFailed
	}

	if history == nil || history.State == types.ProcessStateStopped || history.State == types.ProcessStateUnknown {
		return nil
	}

	cmd.Printf("%s %s %s %s\n\n", ui.LabelWarning.Render("warning"), ui.TextBold.Render(serviceName), "is currently", string(history.State))
	cmd.Printf("%s %s %s\n\n", ui.TextMuted.Render("tip:"), ui.TextCommand.Render(fmt.Sprintf("eos stop %s", serviceName)), ui.TextMuted.Render("→ stop it first"))
	if !helpers.PromptConfirm(cmd, "remove anyway? (y/n):") {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "remove aborted")
		return helpers.ErrCommandFailed
	}

	return nil
}

func removeCmdRemoveInstance(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) error {
	serviceInstance, err := mgr.GetServiceInstance(cmd.Context(), serviceName)
	if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service instance: %v", err))
		return helpers.ErrCommandFailed
	}

	if serviceInstance == nil {
		return nil
	}

	removedInstance, removeInstanceErr := mgr.RemoveServiceInstance(cmd.Context(), serviceName)
	if removeInstanceErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing service instance: %v", removeInstanceErr))
		return helpers.ErrCommandFailed
	}
	if !removedInstance {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "unable to remove service instance")
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "service instance removed")
	return nil
}

func removeCmdRemoveCatalogEntry(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) error {
	removed, err := mgr.RemoveServiceCatalogEntry(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing service: %v", err))
		return helpers.ErrCommandFailed
	}

	if !removed {
		cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "could not be removed")
		return helpers.ErrCommandFailed
	}

	return nil
}

func removeCmdPrintSuccess(cmd *cobra.Command, serviceName string) {
	cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), "unregistered")
	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos list"), ui.TextMuted.Render("→ view registered services"))
}
