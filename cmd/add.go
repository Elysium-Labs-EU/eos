package cmd

import (
	"eos/internal/manager"
	"eos/internal/types"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
				cmd.Printf("Error: Unable to determine YAML file correctly - got: %v", err)
				return
			}
			cmd.Printf("Service details: %+v\n", yamlFile)

			data, err := os.ReadFile(yamlFile)
			if err != nil {
				cmd.Printf("Error reading YAML file: %v:\n", err)
				return
			}

			var config types.ServiceConfig
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				cmd.Printf("Error parsing YAML: %v\n", err)
				return
			}

			absPath, err := filepath.Abs(filepath.Dir(yamlFile))
			if err != nil {
				cmd.Printf("Error getting absolute path: %v\n", err)
				return
			}

			serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(config.Name, absPath, filepath.Base(yamlFile))
			if err != nil {
				cmd.Printf("Create service catalog entry was not able to complete, got: %v", err)
				return
			}

			err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)

			if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
				cmd.Printf("Service '%s' is already registered.\n", config.Name)
				cmd.Printf("Use 'eos remove %s' first to re-register.\n", config.Name)
				return
			}
			if err != nil {
				// TODO: Make the error more explicit type to be more helpful with suggestions.
				cmd.Printf("Error registering service: %v\n", err)
				return
			}

			cmd.Printf("Successfully registered service '%s'\n", config.Name)
			cmd.Printf("Path: %s\n", absPath)
			cmd.Printf("Config: %s\n", filepath.Base(yamlFile))
			cmd.Println()
			// TODO: Add deploy in between if needed
			cmd.Printf("Use 'eos start %s' to start the service\n", config.Name)
		},
	}
}
