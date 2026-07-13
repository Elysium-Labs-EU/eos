package helpers

import (
	"errors"
	"reflect"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"github.com/spf13/cobra"
)

// fakeCatalogMgr implements manager.ServiceManager, only overriding
// GetAllServiceCatalogEntries; all other methods are unused by ServiceNameCompletions.
type fakeCatalogMgr struct {
	manager.ServiceManager
	err     error
	entries []types.ServiceCatalogEntry
}

func (f *fakeCatalogMgr) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	return f.entries, f.err
}

func TestServiceNameCompletions(t *testing.T) {
	t.Run("returns registered service names", func(t *testing.T) {
		mgr := &fakeCatalogMgr{entries: []types.ServiceCatalogEntry{{Name: "foo"}, {Name: "bar"}}}
		fn := ServiceNameCompletions(func() manager.ServiceManager { return mgr })

		names, directive := fn(&cobra.Command{}, nil, "")

		if !reflect.DeepEqual(names, []string{"foo", "bar"}) {
			t.Errorf("expected [foo bar], got %v", names)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected NoFileComp directive, got %v", directive)
		}
	})

	t.Run("no completion once an arg is already given", func(t *testing.T) {
		mgr := &fakeCatalogMgr{entries: []types.ServiceCatalogEntry{{Name: "foo"}}}
		fn := ServiceNameCompletions(func() manager.ServiceManager { return mgr })

		names, directive := fn(&cobra.Command{}, []string{"foo"}, "")

		if names != nil {
			t.Errorf("expected no names, got %v", names)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected NoFileComp directive, got %v", directive)
		}
	})

	t.Run("manager error surfaces as completion error", func(t *testing.T) {
		mgr := &fakeCatalogMgr{err: errors.New("db unavailable")}
		fn := ServiceNameCompletions(func() manager.ServiceManager { return mgr })

		names, directive := fn(&cobra.Command{}, nil, "")

		if names != nil {
			t.Errorf("expected no names, got %v", names)
		}
		if directive != cobra.ShellCompDirectiveError {
			t.Errorf("expected Error directive, got %v", directive)
		}
	})
}
