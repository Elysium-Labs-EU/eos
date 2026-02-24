package cmd

import (
	"bytes"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemConfigCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"system", "config"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing config test - add should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "System Config") {
		t.Error("expected the output to contain 'System Config' header")
	}
}

func TestSystemUpdateCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"system", "update"})

	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing update test - should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "dev") {
		t.Error("expected the output to contain 'dev'")
	}
}

// func TestSystemUpdateWithValidVersionCommand(t *testing.T) {
// 	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	manager := manager.NewLocalManager(db, tempDir, t.Context())
// 	cmd := newTestRootCmd(manager)

// 	t.Setenv("EOS_INSTALL_DIR", tempDir)
// 	cmd.SetContext(t.Context())

// 	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
// 	if err != nil {
// 		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
// 	}

// 	installDir, _, _, err := createSystemConfig()
// 	if err != nil {
// 		t.Fatalf("preparing update test - should not return an error, got: %v\n", err)
// 	}

// 	updateCmd(cmd, "v0.0.1", installDir)
// }

func TestSystemVersionCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"system", "version"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing version test - add should not return an error, got: %v\n", err)
	}

	output := buf.String()
	if !strings.Contains(output, "dev") {
		t.Error("expected the output to contain 'dev'")
	}
}
