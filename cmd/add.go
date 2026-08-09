package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newAddCmd(getManager func() manager.ServiceManager, managerMode localModeFn) *cobra.Command {
	return &cobra.Command{
		Use:   cmdnames.UseAdd,
		Short: "Register a service from a directory",
		Long: `Register a service by providing the path to a directory containing a service configuration.

Registering a service also enables it: it auto-starts on every future daemon
boot (reboot, "eos daemon start", "eos system update") until it's stopped by
hand with "eos stop".`,
		Example: `  eos add ./path/to/project            # find service.yaml automatically in the directory
 eos add ./path/to/project/service.yaml  # point directly to the config file`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]
			mgr := getManager()

			// Registering in-process writes the catalog entry into a state DB a
			// live supervised daemon is also writing.
			if err := refuseLocalWrite(cmd, managerMode()); err != nil {
				return err
			}

			yamlFile, err := helpers.DetermineYamlFile(projectPath)
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("determining YAML file: %v", err))
				return helpers.ErrCommandFailed
			}

			config, errs := manager.ValidateServiceConfig(yamlFile)
			if len(errs) > 0 || config == nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("invalid service config: %v", errors.Join(errs...)))
				return helpers.ErrCommandFailed
			}

			printSelfDetachWarnings(cmd, config.Command)

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving absolute path: %v", err))
				return helpers.ErrCommandFailed
			}

			serviceCatalogEntry, err := manager.NewServiceCatalogEntry(config.Name, absPath, filepath.Base(yamlFile))
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating service catalog entry: %v", err))
				return helpers.ErrCommandFailed
			}

			err = mgr.AddServiceCatalogEntry(cmd.Context(), serviceCatalogEntry)

			if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
				cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(config.Name), "is already registered")
				cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintRemove, config.Name)), ui.TextMuted.Render("first to re-register"))
				return helpers.ErrCommandFailed
			}
			if errors.Is(err, manager.ErrServiceNameCaseConflict) {
				cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(config.Name), "collides with an existing service that differs only in letter case")
				cmd.PrintErrf("  %s\n\n", ui.TextMuted.Render("their log files would share one file on case-insensitive filesystems; pick a distinct name"))
				return helpers.ErrCommandFailed
			}
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("registering service: %v", err))
				return helpers.ErrCommandFailed
			}

			cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), ui.TextBold.Render(config.Name), "registered")
			cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("path:"), absPath)
			cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("config:"), filepath.Base(yamlFile))
			cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintRun, config.Name)), ui.TextMuted.Render("to start the service"))
			cmd.Printf("      %s\n\n", ui.TextCommand.Render(cmdnames.HintStatus))
			return nil
		},
	}
}
