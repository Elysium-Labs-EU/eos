package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

// TestEnvCommandUsageError checks the "usage: eos env ..." fallback for an
// argument shape that isn't list/set/unset (here: two args where the second
// isn't "set" or "unset").
func TestEnvCommandUsageError(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "toggle"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "usage: eos env") {
		t.Errorf("expected usage error, got: %s", errBuf.String())
	}
}

// TestEnvCommandConfigLoadError checks the "loading service config" error
// when the registered service's service.yaml has gone unreadable on disk
// after registration (LoadServiceConfig fails even though the service is
// still registered).
func TestEnvCommandConfigLoadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test file permission restrictions as root")
	}
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v", err)
	}

	if err = os.Chmod(fullPath, 0000); err != nil {
		t.Fatalf("chmod service.yaml: %v", err)
	}
	if data, readErr := os.ReadFile(fullPath); readErr == nil {
		t.Skipf("service.yaml still readable after chmod 0000 (elevated privileges?), got: %q", data)
	}

	cmd.SetArgs([]string{"env", "cms"})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "loading service config") {
		t.Errorf("expected 'loading service config' error, got: %s", errBuf.String())
	}
}

// TestEnvListReadEnvFileMissing checks runEnvList's own "reading env file"
// error when config.EnvFile is set but the file itself has disappeared from
// disk since registration.
func TestEnvListReadEnvFileMissing(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	if err := os.Remove(filepath.Join(serviceDir, ".env")); err != nil {
		t.Fatalf("removing env file: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "reading env file") {
		t.Errorf("expected 'reading env file' error, got: %s", errBuf.String())
	}
}

// TestEnvSetReadEnvFileMissing checks runEnvSet's own "reading env file"
// error, hit after requireEnvFilePath succeeds (env_file is configured) but
// the file itself can't be read.
func TestEnvSetReadEnvFileMissing(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	if err := os.Remove(filepath.Join(serviceDir, ".env")); err != nil {
		t.Fatalf("removing env file: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms", "set", "FOO=baz"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "reading env file") {
		t.Errorf("expected 'reading env file' error, got: %s", errBuf.String())
	}
}

// TestEnvSetWriteEnvFileError checks runEnvSet's "writing env file" error
// when the env file can be read but not written back (permission denied).
func TestEnvSetWriteEnvFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test file permission restrictions as root")
	}
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	envFilePath := filepath.Join(serviceDir, ".env")
	if err := os.Chmod(envFilePath, 0444); err != nil {
		t.Fatalf("chmod env file: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms", "set", "FOO=updated"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		if err == nil {
			t.Skip("skipping: writing a 0444 file succeeded, likely running with elevated privileges")
		}
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "writing env file") {
		t.Errorf("expected 'writing env file' error, got: %s", errBuf.String())
	}
}

// TestEnvUnsetReadEnvFileMissing checks runEnvUnset's own "reading env file"
// error, the unset counterpart of TestEnvSetReadEnvFileMissing.
func TestEnvUnsetReadEnvFileMissing(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	if err := os.Remove(filepath.Join(serviceDir, ".env")); err != nil {
		t.Fatalf("removing env file: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms", "unset", "FOO"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "reading env file") {
		t.Errorf("expected 'reading env file' error, got: %s", errBuf.String())
	}
}

// TestEnvUnsetWriteEnvFileError checks runEnvUnset's "writing env file"
// error, the unset counterpart of TestEnvSetWriteEnvFileError.
func TestEnvUnsetWriteEnvFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test file permission restrictions as root")
	}
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	envFilePath := filepath.Join(serviceDir, ".env")
	if err := os.Chmod(envFilePath, 0444); err != nil {
		t.Fatalf("chmod env file: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms", "unset", "FOO"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		if err == nil {
			t.Skip("skipping: writing a 0444 file succeeded, likely running with elevated privileges")
		}
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "writing env file") {
		t.Errorf("expected 'writing env file' error, got: %s", errBuf.String())
	}
}

// TestEnvRequireEnvFilePathResolveError checks requireEnvFilePath's own
// "resolving env file path" error, hit when config.EnvFile is configured but
// escapes the service directory (ResolveEnvFilePath's traversal guard).
func TestEnvRequireEnvFilePathResolveError(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithEnvFile("../outside.env"))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v", err)
	}

	cmd.SetArgs([]string{"env", "cms", "set", "FOO=bar"})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "resolving env file path") {
		t.Errorf("expected 'resolving env file path' error, got: %s", errBuf.String())
	}
}
