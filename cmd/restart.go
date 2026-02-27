package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/ui"
)

func newRestartCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restarts an active service",
		Long:  "Restarts an active service",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()
			cfg := getConfig()

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

			pid, err := mgr.RestartService(registeredService.Name, cfg.Shutdown.GracePeriod, 200*time.Millisecond)

			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("restarting service: %v", err))
				return
			}
			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("restarted with PID: %d", pid))
			cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)), ui.TextMuted.Render("→ view service info"))
			cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf("eos logs %s", serviceName)), ui.TextMuted.Render("→ view logs"))
			cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
		}}
}
