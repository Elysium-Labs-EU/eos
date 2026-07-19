package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newEnvCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "env <service> [set KEY=VALUE|unset KEY]",
		Short: "Inspect or edit a service's environment variables",
		Long: `Prints the resolved environment variables for a registered service, sourced
from its env_file. Reads directly from disk, so it reflects the current
env_file contents without requiring the service to be running or restarted.

Use "set KEY=VALUE" to add or update a variable in the service's env_file, or
"unset KEY" to remove one. Both require the service to have env_file configured.`,
		Example: `  eos env cms                     # list resolved env vars
  eos env cms set DEBUG=true      # write DEBUG=true to env_file
  eos env cms unset DEBUG         # remove DEBUG from env_file`,
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		Args:              cobra.RangeArgs(1, 3),
		SilenceUsage:      true,
		SilenceErrors:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
				return helpers.ErrCommandFailed
			}
			if !exists {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos add <path>"), ui.TextMuted.Render("to register it"))
				return helpers.ErrCommandFailed
			}

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return helpers.ErrCommandFailed
			}

			configPath := filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName)
			config, err := manager.LoadServiceConfig(configPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("loading service config: %v", err))
				return helpers.ErrCommandFailed
			}

			switch {
			case len(args) == 1:
				return runEnvList(cmd, config, registeredService.DirectoryPath, serviceName)
			case len(args) == 3 && args[1] == "set":
				return runEnvSet(cmd, config, registeredService.DirectoryPath, serviceName, args[2])
			case len(args) == 3 && args[1] == "unset":
				return runEnvUnset(cmd, config, registeredService.DirectoryPath, serviceName, args[2])
			default:
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), `usage: eos env <service> [set KEY=VALUE|unset KEY]`)
				return helpers.ErrCommandFailed
			}
		},
	}
}

func runEnvList(cmd *cobra.Command, config *types.ServiceConfig, serviceDirectoryPath, serviceName string) error {
	envVars, err := manager.ParseEnvFile(config, serviceDirectoryPath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading env file: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "resolved env for", ui.TextBold.Render(serviceName))

	if config.EnvFile == "" {
		cmd.Println(ui.TextMuted.Render("  no env_file configured"))
		cmd.Println("")
		return nil
	}

	if len(envVars) == 0 {
		cmd.Println(ui.TextMuted.Render("  env_file is empty"))
		cmd.Println("")
		return nil
	}

	for _, envVar := range envVars {
		key, value, _ := strings.Cut(envVar, "=")
		helpers.PrintKV(cmd, key, value)
	}
	cmd.Println("")
	return nil
}

func runEnvSet(cmd *cobra.Command, config *types.ServiceConfig, serviceDirectoryPath, serviceName, assignment string) error {
	key, value, found := strings.Cut(assignment, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), `expected KEY=VALUE, got `+fmt.Sprintf("%q", assignment))
		return helpers.ErrCommandFailed
	}

	envFilePath, err := requireEnvFilePath(cmd, config, serviceDirectoryPath, serviceName)
	if err != nil {
		return err
	}

	lines, err := readEnvFileLines(envFilePath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading env file: %v", err))
		return helpers.ErrCommandFailed
	}

	if err := writeEnvFileLines(envFilePath, setEnvFileLine(lines, key, value)); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing env file: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s %s %s\n\n", ui.LabelSuccess.Render("ok"), "set", ui.TextBold.Render(key), "in "+serviceName+"'s env_file")
	return nil
}

func runEnvUnset(cmd *cobra.Command, config *types.ServiceConfig, serviceDirectoryPath, serviceName, key string) error {
	envFilePath, err := requireEnvFilePath(cmd, config, serviceDirectoryPath, serviceName)
	if err != nil {
		return err
	}

	lines, err := readEnvFileLines(envFilePath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading env file: %v", err))
		return helpers.ErrCommandFailed
	}

	updatedLines, removed := unsetEnvFileLine(lines, key)
	if !removed {
		cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(key), "is not set in env_file")
		return helpers.ErrCommandFailed
	}

	if err := writeEnvFileLines(envFilePath, updatedLines); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing env file: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s %s %s\n\n", ui.LabelSuccess.Render("ok"), "unset", ui.TextBold.Render(key), "in "+serviceName+"'s env_file")
	return nil
}

func requireEnvFilePath(cmd *cobra.Command, config *types.ServiceConfig, serviceDirectoryPath, serviceName string) (string, error) {
	if config.EnvFile == "" {
		cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "has no env_file configured")
		cmd.PrintErrf("  %s\n\n", ui.TextMuted.Render("set env_file in the service config first"))
		return "", helpers.ErrCommandFailed
	}

	envFilePath, err := manager.ResolveEnvFilePath(config, serviceDirectoryPath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving env file path: %v", err))
		return "", helpers.ErrCommandFailed
	}
	return envFilePath, nil
}

// envFileLineKey returns the KEY of a "KEY=VALUE" env_file line, or "" if the
// line is blank, a comment, or has no "=".
func envFileLineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	key, _, found := strings.Cut(trimmed, "=")
	if !found {
		return ""
	}
	return key
}

// setEnvFileLine replaces the last line assigning key (dropping any earlier
// duplicates), or appends a new line if key isn't present. Comments, blank
// lines, and unrelated assignments are left untouched.
func setEnvFileLine(lines []string, key, value string) []string {
	assignment := key + "=" + value

	lastIdx := -1
	for i, line := range lines {
		if envFileLineKey(line) == key {
			lastIdx = i
		}
	}
	if lastIdx == -1 {
		return append(lines, assignment)
	}

	updated := make([]string, 0, len(lines))
	for i, line := range lines {
		switch {
		case i == lastIdx:
			updated = append(updated, assignment)
		case envFileLineKey(line) == key:
			continue
		default:
			updated = append(updated, line)
		}
	}
	return updated
}

// unsetEnvFileLine removes every line assigning key, leaving comments and
// blank lines untouched. Reports whether any line was removed.
func unsetEnvFileLine(lines []string, key string) ([]string, bool) {
	updated := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		if envFileLineKey(line) == key {
			removed = true
			continue
		}
		updated = append(updated, line)
	}
	return updated, removed
}

func readEnvFileLines(path string) ([]string, error) {
	// #nosec G304 - path resolved and validated by manager.ResolveEnvFilePath
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n"), nil
}

func writeEnvFileLines(path string, lines []string) error {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return os.WriteFile(filepath.Clean(path), []byte(content), 0o600)
}
