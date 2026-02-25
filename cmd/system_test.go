package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
)

func TestSystemInfoCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"system", "info"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing info test - add should not return an error, got: %v\n", err)
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

func TestCopyFile_TextFileBusy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("text file busy error is Linux-specific")
	}

	tempDir := t.TempDir()

	// Create a minimal executable binary (a simple sleep script won't work;
	// we need an actual ELF binary). We'll compile a tiny Go program.
	srcPath := filepath.Join(tempDir, "main.go")
	err := os.WriteFile(srcPath, []byte(`package main
import "time"
func main() { time.Sleep(30 * time.Second) }
`), 0644)
	if err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	binaryPath := filepath.Join(tempDir, "eos")
	buildCmd := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, srcPath)
	if out, outputErr := buildCmd.CombinedOutput(); outputErr != nil {
		t.Fatalf("failed to compile test binary: %v\n%s", outputErr, out)
	}

	// Start the binary so the OS holds it as "text busy"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	proc := exec.CommandContext(ctx, binaryPath)
	if startErr := proc.Start(); startErr != nil {
		t.Fatalf("failed to start test binary: %v", startErr)
	}
	defer func() {
		cancel()
		_ = proc.Wait()
	}()

	time.Sleep(100 * time.Millisecond)

	replacementPath := filepath.Join(tempDir, "eos_new")
	err = os.WriteFile(replacementPath, []byte("new binary content"), 0755)
	if err != nil {
		t.Fatalf("failed to create replacement file: %v", err)
	}

	err = copyFile(replacementPath, binaryPath)
	if err == nil {
		t.Fatal("expected 'text file busy' error, got nil")
	}

	if !strings.Contains(err.Error(), "text file busy") {
		t.Errorf("expected 'text file busy' in error message, got: %v", err)
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
