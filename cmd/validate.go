package cmd

import (
	"fmt"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a service configuration file",
		Long:  `Validate a service.yaml without registering it or requiring the daemon to run.`,
		Example: `  eos validate ./path/to/project            # find service.yaml automatically in the directory
  eos validate ./path/to/project/service.yaml  # point directly to the config file`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]

			yamlFile, err := helpers.DetermineYamlFile(projectPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("determining YAML file: %v", err))
				return helpers.ErrCommandFailed
			}

			config, errs := manager.ValidateServiceConfig(yamlFile)
			if len(errs) > 0 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("invalid"), yamlFile)
				for _, e := range errs {
					cmd.PrintErrf("  %s %s\n", ui.TextMuted.Render("·"), e)
				}
				cmd.PrintErrf("\n")
				return helpers.ErrCommandFailed
			}

			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("valid"), ui.TextBold.Render(config.Name), "configuration is valid")
			cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("file:"), yamlFile)

			for _, w := range manager.DetectSelfDetachRisk(config.Command) {
				cmd.PrintErrf("%s %s\n", ui.LabelWarning.Render("warning"), w)
			}

			return nil
		},
	}
}
