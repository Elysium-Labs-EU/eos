package cmd

import (
	"bytes"
	"eos/internal/manager"
	"eos/internal/testutil"
	"eos/internal/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRestartCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
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
		t.Fatalf("error occured during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occured during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.Execute()

	if err != nil {
		t.Fatalf("add should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"start", testFile.Name})
	err = cmd.Execute()

	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "Started with PID:") {
		t.Errorf("The start command didn't complete successfully, no PID was returned")
	}

	cmd.SetArgs([]string{"restart", testFile.Name})
	err = cmd.Execute()

	output = buf.String()

	if !strings.Contains(output, "Restarted with PID:") {
		t.Errorf("The restart command didn't complete successfully, no PID was returned")
	}

	pidPrefIndex := strings.Index(output, "PID:")
	if pidPrefIndex == -1 {
		t.Errorf("No PID substring found")
	}
	startIndex := pidPrefIndex + 5
	endIndex := strings.Index(output[startIndex:], "\n")

	if endIndex == -1 {
		endIndex = len(output)
	} else {
		// Making it absolute
		endIndex = startIndex + endIndex
	}

	pidAsString := strings.TrimSpace(output[startIndex:endIndex])
	pidAsInt64, err := strconv.ParseInt(pidAsString, 0, 64)

	if err != nil {
		t.Errorf("Failed to convert PID to number, got: %v", err)
	}
	pidAsInt := int(pidAsInt64)
	signal := syscall.SIGTERM
	err = syscall.Kill(pidAsInt, signal)
	if err != nil {
		t.Errorf("The SIGTERM call failed, got: %v", err)
	}
}
