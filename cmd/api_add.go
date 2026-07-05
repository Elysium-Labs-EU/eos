package cmd

import (
	"fmt"
	"path/filepath"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiAddResult struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ConfigFile string `json:"config_file"`
}

func newAPIAddCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Register a service from a directory; always outputs JSON",
		Long: `Register a service by providing the path to a directory containing a service configuration.

Output schema (stdout, JSON):
  {
    "name":        string  -- service name from config
    "path":        string  -- absolute path to the service directory
    "config_file": string  -- config filename (e.g. service.yaml)
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api add ./path/to/project
  eos api add ./path/to/project/service.yaml
  eos api add ./path/to/project | jq .name`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]
			mgr := getManager()

			parsed, err := parseServiceFile(projectPath)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			result, err := registerServiceIfNeeded(mgr, parsed.YamlFile, parsed.Config.Name)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("registering service: %w", err))
			}
			if result.AlreadyExists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q is already registered", parsed.Config.Name))
			}

			absPath, err := filepath.Abs(filepath.Dir(parsed.YamlFile))
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("resolving path: %w", err))
			}

			return helpers.WriteJSON(cmd, apiAddResult{
				Name:       result.Name,
				Path:       absPath,
				ConfigFile: filepath.Base(parsed.YamlFile),
			})
		},
	}
}
