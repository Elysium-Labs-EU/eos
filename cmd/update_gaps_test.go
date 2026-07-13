package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateCommandServiceNotRegistered(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	newPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v2"))

	cmd.SetArgs([]string{"update", "does-not-exist", newPath})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for an unregistered service")
	}
	if !strings.Contains(errBuf.String(), "isn't registered") {
		t.Errorf("expected 'isn't registered' error, got: %s", errBuf.String())
	}
}

func TestUpdateCommandInvalidPath(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	firstPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v1"))
	cmd.SetArgs([]string{"add", firstPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	cmd.SetArgs([]string{"update", "cms", filepath.Join(tempDir, "does-not-exist")})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for a nonexistent path")
	}
	if !strings.Contains(errBuf.String(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %s", errBuf.String())
	}
}

func TestUpdateCommandMissingArgs(t *testing.T) {
	cmd, _, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"update", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for missing arguments")
	}
}

func TestUpdateCommandTooManyArgs(t *testing.T) {
	cmd, _, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"update", "cms", "/a", "/b"})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for too many arguments")
	}
}
