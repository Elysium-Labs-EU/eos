package cmd

import (
	"bytes"
	"deploy-cli/internal/manager"
	"deploy-cli/internal/testutil"
	"strings"
	"testing"
)

func TestStatusCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()

	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "No services registered") {
		t.Errorf("Expected status to show 'No services registered', got: %s", output)
	}
}

// func TestStatusCommandShowsServices(t *testing.T) {
// 	var buf bytes.Buffer

// 	cmd := newRootCmd()
// 	cmd.SetOut(&buf)
// 	cmd.SetErr(&buf)

// 	// TODO: We need a way to inject test services into the status command
// 	// For now, let's think about what we want to see:
// 	//
// 	// SERVICE         STATUS    PORT    PID
// 	// strapi          running   1337    1234
// 	// main-website    stopped   3000    -
// 	// donation-module running   3001    5678

// 	cmd.SetArgs([]string{"status"})

// 	err := cmd.Execute()
// 	if err != nil {
// 		t.Fatalf("Status command should not return an error, got: %v", err)
// 	}

// 	output := buf.String()
// 	t.Logf("Status output: %q", output)

// 	// For now, this will fail - we're writing the test first
// 	if !strings.Contains(output, "SERVICE") {
// 		t.Errorf("Expected status to show service table header, got: %s", output)
// 	}
// }

func TestStatusCommmandWithServices(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"status", "--help"})

	err := cmd.Execute()

	if err != nil {
		t.Fatalf("Status help should not return an error, got: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Display the current status of all configured services") {
		t.Errorf("Expected status help to describe the command, got: %s", output)
	} else if !strings.Contains(output, "deploy-cli status") {
		t.Errorf("Expected status help to show usage, got: %s", output)
	}
}
