package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newAddCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Register a service from a directory",
		Long:  `Register a service by providing the path to a directory containing a service configuration.`,
		Example: `  eos add ./path/to/project            # find service.yaml automatically in the directory
 eos add ./path/to/project/service.yaml  # point directly to the config file`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]
			mgr := getManager()

			yamlFile, err := helpers.DetermineYamlFile(projectPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("determining YAML file: %v", err))
				return helpers.ErrCommandFailed
			}

			config, errs := manager.ValidateServiceConfig(yamlFile)
			if len(errs) > 0 || config == nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("invalid service config: %v", errors.Join(errs...)))
				return helpers.ErrCommandFailed
			}

			for _, w := range manager.DetectSelfDetachRisk(config.Command) {
				cmd.PrintErrf("%s %s\n", ui.LabelWarning.Render("warning"), w)
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving absolute path: %v", err))
				return helpers.ErrCommandFailed
			}

			serviceCatalogEntry, err := manager.NewServiceCatalogEntry(config.Name, absPath, filepath.Base(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating service catalog entry: %v", err))
				return helpers.ErrCommandFailed
			}

			err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)

			if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(config.Name), "is already registered")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos remove %s", config.Name)), ui.TextMuted.Render("first to re-register"))
				return helpers.ErrCommandFailed
			}
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("registering service: %v", err))
				return helpers.ErrCommandFailed
			}

			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(config.Name), "registered")
			cmd.Printf("  %s %s\n", ui.TextMuted.Render("path:"), absPath)
			cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("config:"), filepath.Base(yamlFile))
			cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos run %s", config.Name)), ui.TextMuted.Render("→ start the service"))
			cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
			return nil
		},
	}
}
