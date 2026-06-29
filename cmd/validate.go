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
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			projectPath := args[0]

			yamlFile, err := helpers.DetermineYamlFile(projectPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("determining YAML file: %v", err))
				return
			}

			config, err := manager.ValidateServiceConfig(yamlFile)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), err)
				return
			}

			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("valid"), ui.TextBold.Render(config.Name), "configuration is valid")
			cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("file:"), yamlFile)
		},
	}
}
