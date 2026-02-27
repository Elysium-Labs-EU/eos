package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
	"eos/internal/types"
)

// TODO: Add actual node env here?
// func TestInfoCommand(t *testing.T) {
// db, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	manager := manager.NewLocalManager(db, tempDir, t.Context())
// 	cmd := newTestRootCmd(manager)
// 	var buf bytes.Buffer
// 	cmd.SetOut(&buf)
// 	cmd.SetErr(&buf)

// 	fullDirPath := filepath.Join(tempDir, "test-project")
// 	err := os.MkdirAll(fullDirPath, 0755)

// 	if err != nil {
// 		t.Fatalf("could not create test-project directory: %v\n", err)
// 	}

// 	runtimePath := filepath.Join(fullDirPath, "bin")

// 	if err != nil {
// 		t.Fatalf("could not create runtime path directory: %v\n", err)
// 	}

// 	runtime := types.Runtime{
// 		Type: "nodejs",
// 		Path: "/test-project/bin",
// 	}
// 	testFile := &types.ServiceConfig{
// 		Name:    "cms",
// 		Command: "/home/user/start-script.sh",
// 		Port:    1337,
// 		Runtime: runtime,
// 	}
// 	yamlData, err := yaml.Marshal(testFile)
// 	if err != nil {
// 		t.Fatalf("Failed to marshal test config: %v", err)
// 	}

// 	fullPath := filepath.Join(fullDirPath, "service.yaml")
// 	os.WriteFile(fullPath, yamlData, 0644)

// 	cmd.SetArgs([]string{"add", fullPath})
// 	err = cmd.ExecuteContext(t.Context())
// 	if err != nil {
// 		t.Fatalf("Add command should not return an error, got: %v\n", err)
// 	}

// 	cmd.SetArgs([]string{"start", "cms"})
// 	err = cmd.ExecuteContext(t.Context())
// 	if err != nil {
// 		t.Fatalf("Start command should not return an error, got : %v", err)
// 	}

// 	cmd.SetArgs([]string{"info", "cms"})
// 	err = cmd.ExecuteContext(t.Context())
// 	if err != nil {
// 		t.Fatalf("Info command should not return an error, got : %v", err)
// 	}

// 	output := buf.String()
// 	if !strings.Contains(output, "Name: cms") {
// 		t.Errorf("Expected name to be 'cms'")
// 	}
// 	if !strings.Contains(output, fmt.Sprintf("Path: %s", fullDirPath)) {
// 		t.Errorf("Expected Path to be '%s'", fmt.Sprintf("Path: %s", fullDirPath))
// 	}
// 	if !strings.Contains(output, "Service command: /home/user/start-script.sh") {
// 		t.Errorf("Expected service command to be '/home/user/start-script.sh'")
// 	}
// 	if !strings.Contains(output, "Service port: 1337") {
// 		t.Errorf("Expected Service port to be '1337'")
// 	}
// 	if !strings.Contains(output, "Runtime: nodejs") {
// 		t.Errorf("Expected runtime to be 'nodejs'")
// 	}
// 	if !strings.Contains(output, "Runtime path: /test-project/bin") {
// 		t.Errorf("Expected runtime path to be '/test-project/bin'")
// 	}
// }

func TestInfoOnlyRegisteredServiceCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	testFile := testutil.CreateTestServiceConfigFile(t)
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"info", "cms"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Info command should not return an error, got : %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "name") || !strings.Contains(output, "cms") {
		t.Errorf("Expected name to be 'cms'")
	}
	if !strings.Contains(output, "path") || !strings.Contains(output, fullDirPath) {
		t.Errorf("Expected path to contain '%s'", fullDirPath)
	}
	if !strings.Contains(output, "command") || !strings.Contains(output, "/home/user/start-script.sh") {
		t.Errorf("Expected command to be '/home/user/start-script.sh'")
	}
	if !strings.Contains(output, "port") || !strings.Contains(output, "1337") {
		t.Errorf("Expected port to be '1337'")
	}
	if !strings.Contains(output, "runtime") || !strings.Contains(output, "nodejs") {
		t.Errorf("Expected runtime to be 'nodejs'")
	}
	if !strings.Contains(output, "runtime path") || !strings.Contains(output, "/path/to/node") {
		t.Errorf("Expected runtime path to be '/path/to/node'")
	}
}

func TestInfoOnlyRegisteredServiceIncompleteCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
	}
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"info", "cms"})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Info command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "command") || !strings.Contains(output, "/home/user/start-script.sh") {
		t.Errorf("Expected command to be present in config section")
	}
	if !strings.Contains(output, "runtime") || !strings.Contains(output, "N/A") {
		t.Errorf("Expected runtime to show 'N/A' for incomplete config, got: %s", output)
	}
	if !strings.Contains(output, "runtime path") || !strings.Contains(output, "N/A") {
		t.Errorf("Expected runtime path to show 'N/A' for incomplete config, got: %s", output)
	}
}

func TestInfoInvalidNumberArgumentsCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"info"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatalf("Info command should return an error")
	}
	output := buf.String()

	if !strings.Contains(output, "Error: accepts 1 arg(s), received 0") {
		t.Errorf("Expected info to show 'Error: accepts 1 arg(s), received 0', got: %s", output)
	}
}

func TestInfoNonExistentServiceCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"info", "cms"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Info command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "service not found") {
		t.Errorf("Expected info to show 'service not found', got: %s", output)
	}
}
