package manager

import (
	"eos/internal/database"
	"eos/internal/testutil"
	"eos/internal/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNewManager(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	if manager == nil {
		t.Fatal("Manager should not be nil")
	} else {
		services, err := manager.GetAllServiceCatalogEntries()
		if err != nil {
			t.Errorf("GetAllRegisteredServices shouldn't error, got: %v\n", err)
		} else if len(services) != 0 {
			t.Errorf("Expected 0 services, got %d", len(services))
		}
	}
}

func TestAddService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	serviceCatalogEntry, err := CreateServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service catalog entry should not error: %v", err)
	} else {
		services, err := manager.GetAllServiceCatalogEntries()
		if err != nil {
			t.Fatalf("Getting all service catalog entries should not error: %v", err)
		}
		if len(services) != 1 {
			t.Errorf("Expected 1 service catalog entry, got %d", len(services))
		}
		if services[0].Name != "test-service" {
			t.Errorf("Expected service name 'test-service', got '%s'", services[0].Name)
		}
	}
}

func TestAddServiceMultipleTimes(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	serviceCatalogEntry, err := CreateServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service should not error: %v", err)
	} else {
		err := manager.AddServiceCatalogEntry(serviceCatalogEntry)

		if err == nil {
			t.Errorf("Expected error on adding the same service catalog entry twice")
		} else if strings.Contains(err.Error(), "service name cannot be empty") {
			t.Errorf("Test failed due to invalid test input, got: %v\n", err)
		}
	}
}

func TestGetService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	serviceCatalogEntry, err := CreateServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	found, error := manager.GetServiceCatalogEntry("test-service")
	if error != nil {
		t.Errorf("Service should exist")
	} else if found.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got %s", found.Name)
	}
}

func TestGetInvalidServiceInstance(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	serviceInstance, error := manager.GetServiceInstance("non-existent")
	if serviceInstance != nil {
		t.Error("Non-existent service should not exist")
	}
	if error != nil {
		t.Error("Non-existent service should throw an error")
	}
}

func TestStartService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir)

	runtime := types.Runtime{
		Type: "nodejs",
	}
	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
		Runtime: runtime,
	}

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-files")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-files directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occured during writing the yaml file, got: %v\n", err)
	}

	serviceCatalogEntry, err := CreateServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pid, err := manager.StartService("test-service")

	if err != nil {
		t.Fatalf("Starting service should not error: %v\n", err)
	} else if pid == 0 {
		t.Fatalf("Starting service should have a failed PID, got: %v\n", err)
	}
}
