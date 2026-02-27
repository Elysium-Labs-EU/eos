package cmd

import (
	"bytes"
	"strings"
	"testing"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
)

func TestRootCommand(t *testing.T) {
	var buf bytes.Buffer

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Root command should not return an error, got: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "eos - Test version") {
		t.Errorf("Expected output to contain 'eos - Test version', got %s", output)
	}
	if !strings.Contains(output, "eos help") {
		t.Errorf("Expected output to contain help text, got: %s", output)
	}
}

func TestHelpCommand(t *testing.T) {
	var buf bytes.Buffer

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{"--help"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Help command should not return an error, go: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "eos is a modern deployment") {
		t.Errorf("Expected help to contain description, got: '%s'", output)
	}
}

func TestCreateSystemConfigHelper(t *testing.T) {
	_, baseDir, _, err := createSystemConfig()

	if err != nil {
		t.Fatalf("Creating the system config should not throw an error")
	}
	if baseDir == "" {
		t.Fatalf("Basedir variable cannot be an empty string")
	}
}
