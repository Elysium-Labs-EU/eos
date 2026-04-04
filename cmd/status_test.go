package cmd

import (
	"strings"
	"testing"
)

func TestStatusCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"status"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}
	output := errBuf.String()

	if !strings.Contains(output, "error no services are registered") {
		t.Errorf("Expected status to show 'error no services are registered', got: %s", output)
	}
}

// TODO: func TestStatusCommandGetCatalogError (requires mock manager)
// TODO: func TestStatusCommandWithRegisteredService (add a service, check table row)
// TODO: func TestStatusCommandConfigLoadError (registered service with missing/invalid yaml)
// TODO: func TestStatusCommandConfigNameMismatch (config name differs from registered name)
// TODO: func TestStatusCommandGetInstanceError (requires mock manager)
// TODO: func TestStatusCommandGetProcessHistoryError (requires mock manager)
// TODO: func TestStatusCommandWithRunningService (service instance present, check memory/uptime/restarts columns)

func TestStatusCommandWithServices(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"status", "--help"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Status help should not return an error, got: %v", err)
	}
	output := outBuf.String()

	if !strings.Contains(output, "Display the current status of all configured services") {
		t.Errorf("Expected status help to describe the command, got: %s", output)
	}
	if !strings.Contains(output, "eos status") {
		t.Errorf("Expected status help to show usage, got: %s", output)
	}
}
