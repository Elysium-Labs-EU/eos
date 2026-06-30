package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func NewServiceCatalogEntry(name string, path string, configFile string) (*types.ServiceCatalogEntry, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("received an empty name for the service")
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("received an empty path for the service")
	}
	if strings.TrimSpace(configFile) == "" {
		return nil, fmt.Errorf("received an empty configFile for the service")
	}

	serviceCatalogEntry := &types.ServiceCatalogEntry{
		Name:           name,
		DirectoryPath:  path,
		ConfigFileName: configFile,
		CreatedAt:      time.Now(),
	}

	return serviceCatalogEntry, nil
}

func LoadServiceConfig(configFilePath string) (*types.ServiceConfig, error) {
	if len(configFilePath) == 0 {
		return nil, fmt.Errorf("configFilePath is empty, got %s", configFilePath)
	}
	cleanedConfigFilePath := filepath.Clean(configFilePath)
	data, err := os.ReadFile(cleanedConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading configFilePath has failed with: %w", err)
	}
	var config types.ServiceConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("yaml parsing failed with: %w", err)
	}
	if config.Name == "" {
		return nil, fmt.Errorf("service name is required in %s", cleanedConfigFilePath)
	}
	if config.Command == "" {
		return nil, fmt.Errorf("service command is required in %s", cleanedConfigFilePath)
	}

	return &config, nil
}

func ValidateRuntimeBinary(runtime types.Runtime) error {
	if runtime.Path != "" {
		return ValidateRuntimePath(runtime)
	}
	switch runtime.Type {
	case "bun":
		if _, err := exec.LookPath("bun"); err != nil {
			return fmt.Errorf("bun not found in system PATH: %w", err)
		}
	case "deno":
		if _, err := exec.LookPath("deno"); err != nil {
			return fmt.Errorf("deno not found in system PATH: %w", err)
		}
	case "node", "nodejs":
		if _, err := exec.LookPath("node"); err != nil {
			return fmt.Errorf("node not found in system PATH: %w", err)
		}
	}
	return nil
}

func ValidateServiceConfig(configFilePath string) (*types.ServiceConfig, []error) {
	if len(configFilePath) == 0 {
		return nil, []error{fmt.Errorf("configFilePath is empty")}
	}
	cleanedConfigFilePath := filepath.Clean(configFilePath)
	data, err := os.ReadFile(cleanedConfigFilePath)
	if err != nil {
		return nil, []error{fmt.Errorf("reading file: %w", err)}
	}
	var config types.ServiceConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, []error{fmt.Errorf("yaml parsing: %w", err)}
	}

	var errs []error
	if config.Name == "" {
		errs = append(errs, fmt.Errorf("service name is required"))
	}
	if config.Command == "" {
		errs = append(errs, fmt.Errorf("service command is required"))
	}
	if err := ValidateRuntimeBinary(config.Runtime); err != nil {
		errs = append(errs, fmt.Errorf("runtime: %w", err))
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return &config, nil
}
