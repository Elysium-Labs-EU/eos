package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/ui"
)

func newStartCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Starts a registered service",
		Long:  "Starts up a registered service",
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
			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if errors.Is(err, database.ErrServiceNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return
			}
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return
			}

			pid, err := mgr.StartService(registeredService.Name)

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting service: %v", err))
				return
			}
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(serviceName), fmt.Sprintf("started with PID: %d", pid))
			cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)), ui.TextMuted.Render("→ view service info"))
			cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf("eos logs %s", serviceName)), ui.TextMuted.Render("→ stream logs"))
			cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
		}}
}
