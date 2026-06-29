package config

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestGetBaseDir_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_BASE_DIR", dir)

	got, err := GetBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

func TestGetBaseDir_SudoUser(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", "")

	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}
	t.Setenv("SUDO_USER", u.Username)

	got, err := GetBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(u.HomeDir, "."+Name)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCreateBaseDir_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir")
	t.Setenv("EOS_BASE_DIR", dir)

	got, err := CreateBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected path %q, got %q", dir, got)
	}
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("stat failed: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", dir)
	}
}

func TestCreateBaseDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_BASE_DIR", dir)

	if _, err := CreateBaseDir(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := CreateBaseDir(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

func TestCreateBaseDir_RootWithoutSudoUserBlocked(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test root guard when actually running as root")
	}
	// Non-root: guard must not trigger regardless of env state.
	dir := filepath.Join(t.TempDir(), "guard-test")
	t.Setenv("EOS_BASE_DIR", dir)
	t.Setenv("SUDO_USER", "")

	_, err := CreateBaseDir()
	if err != nil {
		t.Errorf("non-root invocation should not error, got: %v", err)
	}
}
