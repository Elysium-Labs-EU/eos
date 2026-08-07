package helpers

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// fakeWaitStatusMgr implements manager.ServiceManager (via the embedded nil
// interface, unused here) plus dependencyWaitStatusReader, so
// ResolveDependencyWaitStatus's type assertion succeeds against it.
type fakeWaitStatusMgr struct {
	manager.ServiceManager
	err     error
	status  types.DependencyWaitStatus
	waiting bool
}

func (f *fakeWaitStatusMgr) GetDependencyWaitStatus(context.Context, string) (types.DependencyWaitStatus, bool, error) {
	return f.status, f.waiting, f.err
}

func TestResolveDependencyWaitStatus_waiting(t *testing.T) {
	mgr := &fakeWaitStatusMgr{waiting: true, status: types.DependencyWaitStatus{ServiceName: "web", Pending: []string{"db", "cache"}}}
	got := ResolveDependencyWaitStatus(t.Context(), mgr, "web")
	if !slices.Equal(got, []string{"db", "cache"}) {
		t.Errorf("expected [db cache], got %v", got)
	}
}

func TestResolveDependencyWaitStatus_notWaiting(t *testing.T) {
	mgr := &fakeWaitStatusMgr{waiting: false}
	if got := ResolveDependencyWaitStatus(t.Context(), mgr, "web"); got != nil {
		t.Errorf("expected nil when not waiting, got %v", got)
	}
}

func TestResolveDependencyWaitStatus_readerErrors(t *testing.T) {
	mgr := &fakeWaitStatusMgr{err: errors.New("boom")}
	if got := ResolveDependencyWaitStatus(t.Context(), mgr, "web"); got != nil {
		t.Errorf("expected nil on reader error, got %v", got)
	}
}

// fakeCatalogMgr (completions_test.go) doesn't implement
// dependencyWaitStatusReader — reuse it to prove the type assertion fails
// gracefully instead of panicking.
func TestResolveDependencyWaitStatus_unsupportedManager(t *testing.T) {
	mgr := &fakeCatalogMgr{}
	if got := ResolveDependencyWaitStatus(t.Context(), mgr, "web"); got != nil {
		t.Errorf("expected nil for a manager without dependency-wait support, got %v", got)
	}
}
