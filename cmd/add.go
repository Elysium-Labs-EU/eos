package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"eos/internal/manager"
	"eos/internal/types"
	"eos/internal/ui"
)

func findServiceFileInDirectory(dir string) string {
	candidates := []string{
		"service.yaml",
		"service.yml",
	}

	for _, candidate := range candidates {
		fullPath := filepath.Join(dir, candidate)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}

	return ""
}

func determineYamlFile(projectPath string) (string, error) {
	fileInfo, err := os.Stat(projectPath)

	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("directory or file on path %s does not exist", projectPath)
		}
		return "", fmt.Errorf("unable to stat path %s: %w", projectPath, err)
	}

	if fileInfo.IsDir() {
		yamlFile := findServiceFileInDirectory(projectPath)
		if yamlFile == "" {
			return "", fmt.Errorf("no service.yaml or service.yml found in %s", projectPath)
		} else {
			return yamlFile, nil
		}
	}
	if strings.HasSuffix(projectPath, ".yaml") || strings.HasSuffix(projectPath, ".yml") {
		return projectPath, nil
	}
	return "", fmt.Errorf("provided path is not a directory nor a yaml file")
}

func newAddCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Register a service from a directory",
		Long:  `Register a service by providing the path to a directory containing a service configuration.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			projectPath := args[0]
			mgr := getManager()

			yamlFile, err := determineYamlFile(projectPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("unable to determine YAML file: %v", err))
				return
			}

			data, err := os.ReadFile(filepath.Clean(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading YAML file: %v", err))
				return
			}

			var config types.ServiceConfig
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing YAML: %v", err))
				return
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving absolute path: %v", err))
				return
			}

			serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(config.Name, absPath, filepath.Base(yamlFile))
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating service catalog entry: %v", err))
				return
			}

			err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)

			if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(config.Name), "is already registered")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos remove %s", config.Name)), ui.TextMuted.Render("first to re-register"))
				return
			}
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("registering service: %v", err))
				return
			}

			cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(config.Name), "registered")
			cmd.Printf("  %s %s\n", ui.TextMuted.Render("path:"), absPath)
			cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("config:"), filepath.Base(yamlFile))
			cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos start %s", config.Name)), ui.TextMuted.Render("→ start the service"))
			cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
		},
	}
}
