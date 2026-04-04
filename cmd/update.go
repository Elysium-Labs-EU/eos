package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newUpdateCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:     "update <service-name> <new-path>",
		Short:   "Update a registered service's path",
		Long:    `Update the directory path for an existing registered service.`,
		Example: `  eos update cms /new/path/to/cms`,
		Args:    cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return helpers.ServiceNameCompletions(getManager)(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveDefault // second arg → file path
		},
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			newProjectPath := args[1]
			mgr := getManager()

			cmd.Printf("%s %s → %s\n", ui.LabelInfo.Render("updating"), ui.TextBold.Render(serviceName), newProjectPath)

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
				return
			}
			if !exists {
				cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "service isn't registered.")
				cmd.PrintErrf("  %s %s %s\n",
					ui.TextMuted.Render("run:"),
					ui.TextCommand.Render("eos add <path>"),
					ui.TextMuted.Render("→ register service"),
				)
				cmd.PrintErrf("  %s %s %s\n",
					ui.TextMuted.Render("run:"),
					ui.TextCommand.Render("eos status"),
					ui.TextMuted.Render("→ view registered services"),
				)
				return
			}
			yamlFile, err := helpers.DetermineYamlFile(newProjectPath)

			if err != nil {
				cmd.PrintErrf("%s %s: %v\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(newProjectPath), err)
				return
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting absolute path: %v", err))
				return
			}

			err = mgr.UpdateServiceCatalogEntry(serviceName, absPath, filepath.Base(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("updating service: %v", err))
				return
			}
			cmd.Printf("%s %s\n", ui.LabelSuccess.Render("updated"), ui.TextBold.Render(serviceName))
		}}
}
