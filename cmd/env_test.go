package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func addServiceWithEnvFile(t *testing.T, cmd *cobra.Command, tempDir string, envFileContents string, errBuf *bytes.Buffer) string {
	t.Helper()

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithEnvFile(".env"))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fullDirPath, ".env"), []byte(envFileContents), 0644); err != nil {
		t.Fatalf("Failed to write the .env file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	return fullDirPath
}

func TestEnvListPrintsResolvedVars(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "# comment\nFOO=bar\nBAZ=qux\nFOO=overridden\n", errBuf)

	cmd.SetArgs([]string{"env", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "FOO") || !strings.Contains(output, "overridden") {
		t.Errorf("expected FOO=overridden in output, got: %s", output)
	}
	if !strings.Contains(output, "BAZ") || !strings.Contains(output, "qux") {
		t.Errorf("expected BAZ=qux in output, got: %s", output)
	}
	if strings.Contains(output, "# comment") {
		t.Errorf("expected comment line to be skipped, got: %s", output)
	}
}

func TestEnvListNoEnvFileConfigured(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	cmd.SetArgs([]string{"env", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "no env_file configured") {
		t.Errorf("expected 'no env_file configured', got: %s", output)
	}
}

func TestEnvNonExistentServiceCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"env", "cms"})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v\nerr output: %s", err, errBuf.String())
	}
	output := errBuf.String()
	if !strings.Contains(output, "is not registered") {
		t.Errorf("expected 'is not registered', got: %s", output)
	}
}

func TestEnvSetAddsNewKey(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "set", "NEWVAR=hello"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env set should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "set") {
		t.Errorf("expected confirmation output, got: %s", outBuf.String())
	}

	envFileContents, err := os.ReadFile(filepath.Join(serviceDir, ".env"))
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	if !strings.Contains(string(envFileContents), "NEWVAR=hello") {
		t.Errorf("expected NEWVAR=hello written to env file, got: %s", envFileContents)
	}
	if !strings.Contains(string(envFileContents), "FOO=bar") {
		t.Errorf("expected existing FOO=bar preserved, got: %s", envFileContents)
	}
}

func TestEnvSetUpdatesExistingKey(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "set", "FOO=updated"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env set should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	envFileContents, err := os.ReadFile(filepath.Join(serviceDir, ".env"))
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	if strings.Contains(string(envFileContents), "FOO=bar") || !strings.Contains(string(envFileContents), "FOO=updated") {
		t.Errorf("expected FOO=updated to replace FOO=bar, got: %s", envFileContents)
	}
}

func TestEnvSetInvalidAssignment(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "set", "NOEQUALSSIGN"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "expected KEY=VALUE") {
		t.Errorf("expected 'expected KEY=VALUE' error, got: %s", errBuf.String())
	}
}

func TestEnvSetWithoutEnvFileConfigured(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
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
	if !strings.Contains(errBuf.String(), "has no env_file configured") {
		t.Errorf("expected 'has no env_file configured', got: %s", errBuf.String())
	}
}

func TestEnvUnsetRemovesKey(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\nBAZ=qux\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "unset", "FOO"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env unset should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	envFileContents, err := os.ReadFile(filepath.Join(serviceDir, ".env"))
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	if strings.Contains(string(envFileContents), "FOO=") {
		t.Errorf("expected FOO to be removed, got: %s", envFileContents)
	}
	if !strings.Contains(string(envFileContents), "BAZ=qux") {
		t.Errorf("expected BAZ=qux preserved, got: %s", envFileContents)
	}
}

func TestEnvUnsetMissingKey(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)

	cmd.SetArgs([]string{"env", "cms", "unset", "NOPE"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "is not set in env_file") {
		t.Errorf("expected 'is not set in env_file', got: %s", errBuf.String())
	}
}

func TestEnvSetPreservesCommentsAndBlankLines(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	original := "# top comment\n\nFOO=bar\n\n# another comment\nBAZ=qux\n"
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, original, errBuf)

	cmd.SetArgs([]string{"env", "cms", "set", "FOO=updated"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env set should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	envFileContents, err := os.ReadFile(filepath.Join(serviceDir, ".env"))
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	got := string(envFileContents)

	for _, want := range []string{"# top comment", "# another comment", "BAZ=qux", "FOO=updated"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive the set, got: %s", want, got)
		}
	}
	if strings.Contains(got, "FOO=bar") {
		t.Errorf("expected FOO=bar to be replaced, got: %s", got)
	}
	// Blank lines and comment lines must survive untouched, in their original positions.
	wantLines := []string{"# top comment", "", "FOO=updated", "", "# another comment", "BAZ=qux"}
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("expected %d lines, got %d: %v", len(wantLines), len(gotLines), gotLines)
	}
	for i, want := range wantLines {
		if gotLines[i] != want {
			t.Errorf("line %d: expected %q, got %q", i, want, gotLines[i])
		}
	}
}

func TestEnvUnsetPreservesCommentsAndBlankLines(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	original := "# top comment\n\nFOO=bar\n\n# another comment\nBAZ=qux\n"
	serviceDir := addServiceWithEnvFile(t, cmd, tempDir, original, errBuf)

	cmd.SetArgs([]string{"env", "cms", "unset", "FOO"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("env unset should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	envFileContents, err := os.ReadFile(filepath.Join(serviceDir, ".env"))
	if err != nil {
		t.Fatalf("could not read env file: %v", err)
	}
	got := string(envFileContents)

	for _, want := range []string{"# top comment", "# another comment", "BAZ=qux"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive the unset, got: %s", want, got)
		}
	}
	if strings.Contains(got, "FOO=") {
		t.Errorf("expected FOO to be removed, got: %s", got)
	}
	wantLines := []string{"# top comment", "", "", "# another comment", "BAZ=qux"}
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("expected %d lines, got %d: %v", len(wantLines), len(gotLines), gotLines)
	}
	for i, want := range wantLines {
		if gotLines[i] != want {
			t.Errorf("line %d: expected %q, got %q", i, want, gotLines[i])
		}
	}
}
