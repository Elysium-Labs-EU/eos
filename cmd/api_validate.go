package cmd

import (
	"fmt"
	"path/filepath"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiValidateResult struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	ConfigFile string   `json:"config_file"`
	Errors     []string `json:"errors,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Valid      bool     `json:"valid"`
}

func newAPIValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a service config file; always outputs JSON",
		Long: `Validate a service.yaml without registering it or requiring the daemon.

Output schema (stdout, JSON):
  {
    "valid":       bool      -- true if config is valid
    "name":        string    -- service name from config (empty if parse failed)
    "path":        string    -- absolute path to the service directory
    "config_file": string    -- config filename
    "errors":      []string  -- validation errors (omitted when valid)
    "warnings":    []string  -- non-fatal warnings, e.g. self-detaching commands (omitted when none)
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success (even when config is invalid — check "valid" field)
  1  error (file not found, cannot parse path)`,
		Example: `  eos api validate ./path/to/project
  eos api validate ./path/to/project/service.yaml
  eos api validate ./path/to/project | jq .valid`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]

			yamlFile, err := helpers.DetermineYamlFile(projectPath)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("determining config file: %w", err))
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("resolving path: %w", err))
			}

			config, errs := manager.ValidateServiceConfig(yamlFile)

			result := apiValidateResult{
				Path:       absPath,
				ConfigFile: filepath.Base(yamlFile),
				Valid:      len(errs) == 0,
			}
			if config != nil {
				result.Name = config.Name
				result.Warnings = manager.DetectSelfDetachRisk(config.Command)
			}
			if len(errs) > 0 {
				result.Errors = make([]string, len(errs))
				for i, e := range errs {
					result.Errors[i] = e.Error()
				}
			}

			return helpers.WriteJSON(cmd, result)
		},
	}
}
