package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

// TestAddCommandAlreadyRegistered covers the ErrServiceAlreadyRegistered
// branch (cmd/add.go:56-59): registering the same service twice must fail
// with the "already registered" message and the "eos remove" re-register hint.
func TestAddCommandAlreadyRegistered(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	path := newYamlServiceFile(t, filepath.Join(tempDir, "project-v1"))

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("first add: unexpected error: %v", err)
	}

	errBuf.Reset()
	cmd.SetArgs([]string{"add", path})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := errBuf.String()
	if !strings.Contains(output, "is already registered") {
		t.Errorf("expected 'is already registered', got: %s", output)
	}
	if !strings.Contains(output, "eos remove cms") {
		t.Errorf("expected re-register hint 'eos remove cms', got: %s", output)
	}
}

// TestAddCommandCaseConflict covers the ErrServiceNameCaseConflict branch
// (cmd/add.go:61-64): a second service whose name differs from an already
// registered one only by letter case must be rejected, since their log
// files would otherwise alias on a case-insensitive filesystem.
func TestAddCommandCaseConflict(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	firstPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v1"))
	cmd.SetArgs([]string{"add", firstPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("first add: unexpected error: %v", err)
	}

	secondDir := filepath.Join(tempDir, "project-v2")
	if err := os.MkdirAll(secondDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithName("CMS"))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	secondPath := filepath.Join(secondDir, "service.yaml")
	if err = os.WriteFile(secondPath, yamlData, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	errBuf.Reset()
	cmd.SetArgs([]string{"add", secondPath})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := errBuf.String()
	if !strings.Contains(output, "collides with an existing service") {
		t.Errorf("expected case-collision message, got: %s", output)
	}
}

// TestAddCommandRegisterDBError covers the generic AddServiceCatalogEntry
// error branch (cmd/add.go:66-69), which is neither ErrServiceAlreadyRegistered
// nor ErrServiceNameCaseConflict. Closing the DB connection out from under the
// manager makes the first internal check inside AddServiceCatalogEntry
// (IsServiceRegistered) fail with a plain DB error, same fault-injection
// pattern as TestSnapshotSaveCommandServiceInstanceLookupError.
func TestAddCommandRegisterDBError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	path := newYamlServiceFile(t, filepath.Join(tempDir, "project"))

	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("closing db: %v", err)
	}

	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"add", path})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "registering service") {
		t.Errorf("expected 'registering service' error, got: %s", errBuf.String())
	}
}

// Not tested, and why:
//
// cmd/add.go:44 (filepath.Abs error resolving the yaml file's directory) —
// filepath.Abs only fails when os.Getwd() fails (e.g. the current working
// directory has been deleted out from under the process). Reproducing that
// safely would require os.Chdir-ing the whole test binary into a directory
// and then removing it, which mutates global process state shared by every
// concurrently running test in this package — not a safe/isolated fault to
// inject here.
//
// cmd/add.go:50 (manager.NewServiceCatalogEntry error) — unreachable from
// this call site. Its only failure modes are ValidateServiceName (already
// run on the identical config.Name two lines earlier via
// manager.ValidateServiceConfig, so any failure there returns before this
// line is reached) and an empty path/configFile, which absPath and
// filepath.Base(yamlFile) never produce.
