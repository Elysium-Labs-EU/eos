package cmd

import (
	"bytes"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
	"strings"
	"testing"
)

func TestRootCommand(t *testing.T) {
	var buf bytes.Buffer

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()

	if err != nil {
		t.Fatalf("Root command should not return an error, got: %v", err)
	} else {
		output := buf.String()

		t.Logf("Buffer length: %d", len(output))
		t.Logf("Buffer content: %q", output)

		if !strings.Contains(output, "eos - Test version") {
			t.Errorf("Expected output to contain 'eos - Test version', got %s", output)
		} else if !strings.Contains(output, "Use 'eos help'") {
			t.Errorf("Expected output to contain help text, got: %s", output)
		}
	}
}

func TestHelpCommand(t *testing.T) {
	var buf bytes.Buffer

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Help command sohuld not return an error, go: %v", err)
	} else {
		output := buf.String()

		t.Logf("Buffer length: %d", len(output))
		t.Logf("Buffer content: %q", output)

		if !strings.Contains(output, "eos is a modern deployment") {
			t.Errorf("Expected help to contain description, got: '%s'", output)
		}
	}
}
