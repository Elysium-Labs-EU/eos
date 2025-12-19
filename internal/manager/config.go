package manager

import (
	"deploy-cli/internal/types"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func CreateServiceCatalogEntry(name string, path string, configFile string) (*types.ServiceCatalogEntry, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("received an empty name for the service")
	} else if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("received an empty path for the service")
	} else if strings.TrimSpace(configFile) == "" {
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
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading configFilePath has failed with: %v", err)
	}
	var config types.ServiceConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("yaml parsing failed with: %v", err)
	} else if config.Name == "" {
		return nil, fmt.Errorf("service name is required in %s", configFilePath)
	} else if config.Command == "" {
		return nil, fmt.Errorf("service command is required in %s", configFilePath)
	} else if config.Runtime.Type == "" {
		return nil, fmt.Errorf("service runtime type is required in %s", configFilePath)
	} else {
		return &config, nil
	}
}
