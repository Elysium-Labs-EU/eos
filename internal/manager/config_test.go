package manager

import (
	"eos/internal/types"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TODO: Rewrite
func TestLoadServiceConfig(t *testing.T) {
	runtime := types.Runtime{
		Type: "nodejs",
	}
	expectedConfig := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
		Runtime: runtime,
	}

	yamlData, err := yaml.Marshal(expectedConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")

	err = os.WriteFile(configFile, yamlData, 0644)
	if err != nil {
		t.Fatalf("LoadingProjectCofnig should not error: %v", err)
	}

	config, err := LoadServiceConfig(configFile)
	if err != nil {
		t.Fatalf("LoadServiceConfig should not error: %v", err)
	}
	if config.Name != "cms" {
		t.Errorf("Expected name 'cms', got %s", config.Name)
	}
	if config.Command != "/home/user/start-script.sh" {
		t.Errorf("Expected command '/home/user/start-script.sh' got '%s'", config.Command)
	}
	if config.Port != 1337 {
		t.Errorf("Expected port 1337, got %d", config.Port)
	}
}

// TODO: Rewrite
func TestLoadServiceConfigWithOptionalFields(t *testing.T) {
	runtime := types.Runtime{
		Type: "nodejs",
	}
	expectedConfig := &types.ServiceConfig{
		Name:    "website",
		Command: "pnpm start",
		Port:    3000,
		Runtime: runtime,
	}
	yamlData, err := yaml.Marshal(expectedConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")

	err = os.WriteFile(configFile, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	config, err := LoadServiceConfig(configFile)
	if err != nil {
		t.Fatalf("LoadServiceConfig should not error: %v", err)
	}
	if config.Port != 3000 {
		t.Errorf("Expected port '3000' got '%d'", config.Port)
	}
}

func TestLoadServiceConfigFileNotFound(t *testing.T) {
	_, err := LoadServiceConfig("non-existent-file.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}

func TestCreateServiceCatalogEntry(t *testing.T) {
	_, err := CreateServiceCatalogEntry("website", "./test-files", "service.yaml")
	if err != nil {
		t.Errorf("TestCreateServiceCatalogEntry should not error: %v", err)
	}
}

func TestCreateServiceCatalogEntryWithEmptyName(t *testing.T) {
	_, err := CreateServiceCatalogEntry("", "./test-files", "service.yaml")
	if err == nil {
		t.Errorf("TestCreateServiceCatalogEntry should error on empty name")
	}
}

func TestCreateServiceCatalogEntryWithEmptyPath(t *testing.T) {
	_, err := CreateServiceCatalogEntry("website", "", "service.yaml")
	if err == nil {
		t.Errorf("TestCreateServiceCatalogEntry should error on empty path")
	}
}

func TestCreateServiceCatalogEntryWithEmptyConfigFile(t *testing.T) {
	_, err := CreateServiceCatalogEntry("website", "./test-files", "")
	if err == nil {
		t.Errorf("TestCreateServiceCatalogEntry should error on empty configFile")
	}
}
