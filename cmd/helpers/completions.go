package helpers

import (
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

// ServiceNameCompletions returns a ValidArgsFunction that completes the first
// positional argument with the names of all registered services.
func ServiceNameCompletions(getManager func() manager.ServiceManager) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		mgr := getManager()
		entries, err := mgr.GetAllServiceCatalogEntries()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
