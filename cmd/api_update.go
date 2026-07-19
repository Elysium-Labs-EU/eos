package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiUpdateResult struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ConfigFile string `json:"config_file"`
}

func newAPIUpdateCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "update <name> <new-path>",
		Short: "Update a service's directory path; always outputs JSON",
		Long: `Update the directory path for an existing registered service.

Output schema (stdout, JSON):
  {
    "name":        string  -- service name
    "path":        string  -- new absolute directory path
    "config_file": string  -- config filename in the new directory
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api update myservice /new/path/to/project
  eos api update myservice /new/path | jq .path`,
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			newProjectPath := args[1]
			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service: %w", err))
			}
			if !exists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q not found", serviceName))
			}

			yamlFile, err := helpers.DetermineYamlFile(newProjectPath)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("determining config file: %w", err))
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("resolving path: %w", err))
			}

			configFile := filepath.Base(yamlFile)
			if err := mgr.UpdateServiceCatalogEntry(serviceName, absPath, configFile); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("updating service: %w", err))
			}

			return helpers.WriteJSON(cmd, apiUpdateResult{
				Name:       serviceName,
				Path:       absPath,
				ConfigFile: configFile,
			})
		},
	}
}
