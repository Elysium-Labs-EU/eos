package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
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

func TestRunWithAmbiguousCommand(t *testing.T) {
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

// Exercises the run command's dependency-gating error path end to end: a
// registered service whose depends_on target never starts must fail loud
// once max_wait elapses, surfaced through the standard "error" output.
func TestRunWithDependsOnMaxWaitFailsLoud(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	dir := filepath.Join(tempDir, "web")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("could not create web directory: %v\n", err)
	}

	cfg := &types.ServiceConfig{
		Name: "web", Command: "/bin/true", DependsOn: []string{"never-started"}, MaxWait: "150ms",
	}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	yamlPath := filepath.Join(dir, "service.yaml")
	if err = os.WriteFile(yamlPath, yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	if err = registerService(mgr, yamlPath, cfg.Name); err != nil {
		t.Fatalf("failed to register service: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"run", cfg.Name})

	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "never-started") {
		t.Fatalf("expected the unmet dependency named in the error output, got: %v", output)
	}
}

// runFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods run.go's helpers call, so
// error branches unreachable through the real LocalManager can be exercised.
type runFakeManager struct {
	manager.ServiceManager
	registeredErr error
	instanceErr   error
	catalogErr    error
	startErr      error
	restartErr    error
	instance      *types.ServiceInstance
	catalogEntry  types.ServiceCatalogEntry
	startPGID     int
	restartPGID   int
	registered    bool
}

func (f *runFakeManager) IsServiceRegistered(_ string) (bool, error) {
	return f.registered, f.registeredErr
}

func (f *runFakeManager) GetServiceInstance(_ string) (*types.ServiceInstance, error) {
	return f.instance, f.instanceErr
}

func (f *runFakeManager) GetServiceCatalogEntry(_ string) (types.ServiceCatalogEntry, error) {
	return f.catalogEntry, f.catalogErr
}

func (f *runFakeManager) StartService(_ string) (int, error) {
	return f.startPGID, f.startErr
}

func (f *runFakeManager) RestartService(_ string, _ time.Duration, _ time.Duration) (int, error) {
	return f.restartPGID, f.restartErr
}

func TestRunParseFlagsErrors(t *testing.T) {
	bare := &cobra.Command{}

	if _, _, err := runParseFlags(bare); err == nil {
		t.Fatal("expected an error parsing the unregistered file flag, got nil")
	}

	bare.Flags().StringP("file", "f", "", "")
	if _, _, err := runParseFlags(bare); err == nil {
		t.Fatal("expected an error parsing the unregistered once flag, got nil")
	}
}

func TestRunGetRegisteredServiceError(t *testing.T) {
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	fake := &runFakeManager{catalogErr: errors.New("boom")}
	if _, err := runGetRegisteredService(cmd, fake, "svc"); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
}

func TestRunResolveServiceNameFromArgsError(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	fake := &runFakeManager{registeredErr: errors.New("db down")}
	if _, err := runResolveServiceNameFromArgs(cmd, fake, "svc"); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
}

func TestRunHandleOnceFlagError(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	fake := &runFakeManager{instanceErr: errors.New("db down")}
	if _, err := runHandleOnceFlag(cmd, fake, true, "svc"); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
}

func TestRunStartRegisteredServiceError(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	fake := &runFakeManager{startErr: errors.New("spawn failed")}
	entry := types.ServiceCatalogEntry{Name: "svc"}
	if err := runStartRegisteredService(cmd, fake, 0, entry); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
}

func TestRunValidArgs(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	rootCmd := newTestRootCmd(mgr)
	runCmd, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("could not find run command: %v", err)
	}

	t.Run("args already present skips completion", func(t *testing.T) {
		completions, directive := runValidArgs(runCmd, []string{"already-set"}, "", func() manager.ServiceManager { return mgr })
		if completions != nil || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("expected (nil, NoFileComp), got (%v, %v)", completions, directive)
		}
	})

	t.Run("file flag set defers to shell file completion", func(t *testing.T) {
		if err := runCmd.Flags().Set("file", "some/path.yaml"); err != nil {
			t.Fatalf("failed to set file flag: %v", err)
		}
		t.Cleanup(func() { _ = runCmd.Flags().Set("file", "") })

		completions, directive := runValidArgs(runCmd, nil, "", func() manager.ServiceManager { return mgr })
		if completions != nil || directive != cobra.ShellCompDirectiveDefault {
			t.Fatalf("expected (nil, Default), got (%v, %v)", completions, directive)
		}
	})

	t.Run("no args and no file flag completes registered service names", func(t *testing.T) {
		testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
		if err := registerService(mgr, filepath.Join(tempDir, "service.yaml"), testFile.Name); err != nil {
			t.Fatalf("failed to register service: %v", err)
		}

		completions, directive := runValidArgs(runCmd, nil, "", func() manager.ServiceManager { return mgr })
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("expected NoFileComp directive, got: %v", directive)
		}
		found := false
		for _, c := range completions {
			if c == testFile.Name {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected completions to include %q, got: %v", testFile.Name, completions)
		}
	})
}
