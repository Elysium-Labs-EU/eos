package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
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

// fakeUpdateMgr implements manager.ServiceManager, overriding only the two
// methods newUpdateCmd's RunE calls; every other call panics via the nil
// embedded interface (same idiom as cmd/snapshot_test.go's fakeSnapshotMgr).
// It exists to force IsServiceRegistered/UpdateServiceCatalogEntry to return
// an error directly, which a real single-connection sqlite-backed manager
// can't do selectively for one call without affecting the other.
type fakeUpdateMgr struct {
	manager.ServiceManager
	isRegisteredErr error
	updateErr       error
	isRegistered    bool
}

func (f *fakeUpdateMgr) IsServiceRegistered(string) (bool, error) {
	return f.isRegistered, f.isRegisteredErr
}

func (f *fakeUpdateMgr) UpdateServiceCatalogEntry(string, string, string) error {
	return f.updateErr
}

// TestUpdateCommandIsRegisteredError covers the mgr.IsServiceRegistered
// error branch (cmd/update.go:36-39). This was the first of two TODOs left
// in update_test.go as requiring a mock manager.
func TestUpdateCommandIsRegisteredError(t *testing.T) {
	mgr := &fakeUpdateMgr{isRegisteredErr: errors.New("registration check exploded")}
	cmd := newTestRootCmd(mgr)

	var errBuf bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"update", "cms", "/some/path"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service") {
		t.Errorf("expected 'checking service' error, got: %s", errBuf.String())
	}
}

// TestUpdateCommandUpdateCatalogError covers the
// mgr.UpdateServiceCatalogEntry error branch (cmd/update.go:67-70). This was
// the second of two TODOs left in update_test.go as requiring a mock manager.
func TestUpdateCommandUpdateCatalogError(t *testing.T) {
	newPath := newYamlServiceFile(t, t.TempDir())
	mgr := &fakeUpdateMgr{isRegistered: true, updateErr: errors.New("catalog update exploded")}
	cmd := newTestRootCmd(mgr)

	var errBuf bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"update", "cms", newPath})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "updating service") {
		t.Errorf("expected 'updating service' error, got: %s", errBuf.String())
	}
}

// Not tested, and why:
//
// cmd/update.go:63 (filepath.Abs error resolving the new path's directory)
// — same as add.go's and init.go's identical pattern: filepath.Abs only
// fails when os.Getwd() fails, which would require deleting the test
// binary's own working directory, an unsafe mutation of global process
// state shared with every other test in this package.
