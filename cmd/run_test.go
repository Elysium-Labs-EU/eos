package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestRunWithServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
	cmd := newTestRootCmd(manager)

	// Needs a genuinely long-lived process: StartService now verifies OS
	// liveness before reporting "already running" (#96), so a command that
	// exits immediately (like "./start-script.sh") would already be dead by
	// the second run and get self-healed into a fresh start instead of a
	// restart.
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("sleep 30"), testutil.WithoutRuntime())

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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondOutput := outBuf.String()
	if !strings.Contains(secondOutput, "restarted with PGID:") {
		t.Fatalf("didn't complete successfully, no PGID was returned, got: %v", secondOutput)
	}
}

func TestRunWithNameUnregisteredCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	cmd.SetArgs([]string{"run", testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "is not registered") {
		t.Fatalf("expected run command to fail with 'is not registered', got: %v", output)
	}
}

func TestRunWithAmbigiousCommand(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	fullPathYaml := filepath.Join(tempDir, "test-project", "service.yaml")

	cmd.SetArgs([]string{"run", "-f", fullPathYaml, testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "error ambiguous input: --file and a service name cannot be used together") {
		t.Fatalf("expected 'error ambiguous input: --file and a service name cannot be used together', got: %v", output)
	}
}

func TestRunWithEmptyCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"run"})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "error no service specified") {
		t.Fatalf("expected 'error no service specified', got: %v", output)
	}
}

func TestRunWithOnceFlagFreshServiceFileCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

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

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithOnceFlagExistingServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}

	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondErrOutput := errBuf.String()
	if !strings.Contains(secondErrOutput, "is already registered") {
		t.Fatalf("expected service to checked and registered, got: %v", secondErrOutput)
	}
	if !strings.Contains(secondErrOutput, "is already running") {
		t.Fatalf("expected service to be running, got: %v", secondErrOutput)
	}
}

func TestRunWithOnceFlagServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	secondErrOutput := errBuf.String()
	if strings.Contains(secondErrOutput, "is already registered") {
		t.Fatalf("expected no service check, got: %v", secondErrOutput)
	}
	if !strings.Contains(secondErrOutput, "is already running") {
		t.Fatalf("expected service to be running, got: %v", secondErrOutput)
	}
}

func TestRunWithOnceFlagServiceNameUnregisteredCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "is not registered") {
		t.Fatalf("expected service to not be registered, got: %v", output)
	}
}

func TestRunWithFileNotFound(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"run", "-f", "-"})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected service file to not be found, got: %v", output)
	}
}

func TestRunWithUnreadableYamlFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test file permission restrictions as root")
	}
	cmd, _, errBuf, tempDir := setupCmd(t)

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

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected service file to be inaccessible, got: %v", output)
	}
}

func TestRunWithOnceFlagStoppedServiceFileCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := outBuf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := outBuf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithOnceFlagStoppedServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := outBuf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := outBuf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithStoppedServiceNameCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(manager.WaitPipes)
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

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"stop", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("stop should not return an error, got: %v\n", err)
	}

	secondOutput := outBuf.String()
	if !strings.Contains(secondOutput, "service instance cleaned up") {
		t.Fatalf("expected service instance to be cleaned up, got: %v", secondOutput)
	}

	outBuf.Reset()
	errBuf.Reset()
	cmd = newTestRootCmd(manager)

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", testFile.Name})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	thirdOutput := outBuf.String()
	if !strings.Contains(thirdOutput, "started with PGID:") {
		t.Fatal("didn't complete successfully, no PGID was returned")
	}
}

func TestRunWithFileParseError(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

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

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "error parsing service file") {
		t.Fatalf("expected parse error, got: %v", output)
	}
}

func TestRunWithFileInvalidConfigRejected(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	testFile.Command = ""

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"run", "-f", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "invalid service config") {
		t.Errorf("expected 'invalid service config' in output, got: %s", output)
	}
}

// When -f targets an already-registered service name, the original catalog entry
// (path and config) is kept; the newly parsed file is only used to resolve the name.
func TestRunWithFileAlreadyRegisteredKeepsOriginalConfig(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	originalFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	originalYaml, err := yaml.Marshal(originalFile)
	if err != nil {
		t.Fatalf("Failed to marshal original config: %v", err)
	}

	originalDir := filepath.Join(tempDir, "original-project")
	err = os.MkdirAll(originalDir, 0755)
	if err != nil {
		t.Fatalf("could not create original-project directory: %v\n", err)
	}
	originalYamlPath := filepath.Join(originalDir, "service.yaml")
	err = os.WriteFile(originalYamlPath, originalYaml, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the original yaml file, got: %v\n", err)
	}
	originalScriptPath := filepath.Join(originalDir, "start-script.sh")
	err = os.WriteFile(originalScriptPath, []byte("#!/bin/bash\necho ORIGINAL"), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the original start script, got: %v\n", err)
	}

	cmd.SetArgs([]string{"run", "--once", "-f", originalYamlPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	// Second file, same service name ("cms"), different directory/config.
	updatedFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	updatedYaml, err := yaml.Marshal(updatedFile)
	if err != nil {
		t.Fatalf("Failed to marshal updated config: %v", err)
	}
	updatedDir := filepath.Join(tempDir, "updated-project")
	err = os.MkdirAll(updatedDir, 0755)
	if err != nil {
		t.Fatalf("could not create updated-project directory: %v\n", err)
	}
	updatedYamlPath := filepath.Join(updatedDir, "service.yaml")
	err = os.WriteFile(updatedYamlPath, updatedYaml, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the updated yaml file, got: %v\n", err)
	}

	var outBuf, errBuf bytes.Buffer
	cmd = newTestRootCmd(mgr)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", "--once", "-f", updatedYamlPath})

	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("run should not return an error, got: %v\n", err)
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "is already registered") {
		t.Fatalf("expected 'is already registered' warning, got: %v", errOutput)
	}
	if !strings.Contains(errOutput, "eos update") {
		t.Fatalf("expected warning to suggest 'eos update', got: %v", errOutput)
	}

	entry, err := mgr.GetServiceCatalogEntry("cms")
	if err != nil {
		t.Fatalf("failed to get catalog entry: %v", err)
	}
	if entry.DirectoryPath != originalDir {
		t.Errorf("expected catalog entry to keep original dir %q, got: %q", originalDir, entry.DirectoryPath)
	}
}
