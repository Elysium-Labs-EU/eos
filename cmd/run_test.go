package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestRunWithServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	buf.Reset()
	cmd = newTestRootCmd(manager)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if !strings.Contains(secondOutput, "restarted with PGID:") {
		t.Fatalf("didn't complete successfully, no PGID was returned, got: %v", secondOutput)
	}
}

func TestRunWithNameUnregisteredCommand(t *testing.T) {
	cmd, buf, _ := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	cmd.SetArgs([]string{"run", testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	t.Logf("output: %v", output)
	if !strings.Contains(output, "is not registered") {
		t.Fatalf("expected run command to fail with 'is not registered', got: %v", output)
	}
}

func TestRunWithAmbigiousCommand(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	fullPathYaml := filepath.Join(tempDir, "test-project", "service.yaml")

	cmd.SetArgs([]string{"run", "-f", fullPathYaml, testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error ambiguous input: --file and a service name cannot be used together") {
		t.Fatalf("expected 'error ambiguous input: --file and a service name cannot be used together', got: %v", output)
	}
}

func TestRunWithEmptyCommand(t *testing.T) {
	cmd, buf, _ := setupCmd(t)
	cmd.SetArgs([]string{"run"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error no service specified") {
		t.Fatalf("expected 'error no service specified', got: %v", output)
	}
}

func TestRunWithOnceFlagFreshServiceFileCommand(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithOnceFlagExistingServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}

	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if !strings.Contains(secondOutput, "is already registered") {
		t.Fatal("expected service to checked and registered")
	}
	if !strings.Contains(secondOutput, "is already running") {
		t.Fatal("expected service to be running")
	}
}

func TestRunWithOnceFlagServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	buf.Reset()
	cmd = newTestRootCmd(manager)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if strings.Contains(secondOutput, "is already registered") {
		t.Fatalf("expected no service check, got: %v", secondOutput)
	}
	if !strings.Contains(secondOutput, "is already running") {
		t.Fatalf("expected service to be running, got: %v", secondOutput)
	}
}

func TestRunWithOnceFlagServiceNameUnregisteredCommand(t *testing.T) {
	cmd, buf, _ := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "is not registered") {
		t.Fatalf("expected service to not be registered, got: %v", output)
	}
}

func TestRunWithFileNotFound(t *testing.T) {
	cmd, buf, _ := setupCmd(t)
	cmd.SetArgs([]string{"run", "-f", "-"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected service file to not be found, got: %v", output)
	}
}

func TestRunWithInvalidYamlFile(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	err = os.Chmod(fullPathYaml, 0000)
	if err != nil {
		t.Fatalf("could not chmod file: %v", err)
	}

	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected service file to be inaccessible, got: %v", output)
	}
}

func TestRunWithOnceFlagStoppedServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := buf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithOnceFlagStoppedServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := buf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithStoppedServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash 
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := buf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	buf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"run", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := buf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithFileParseError(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	fullDirPath := filepath.Join(tempDir, "test-project")
	err := os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, []byte("invalid: yaml: {{{"), 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected parse error, got: %v", output)
	}
}
