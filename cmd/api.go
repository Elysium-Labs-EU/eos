package cmd

import (
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"github.com/spf13/cobra"
)

func newAPICmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	apiCmd := &cobra.Command{
		Use:           "api",
		Short:         "Machine-readable JSON interface",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	apiCmd.AddCommand(newAPIInfoCmd(getManager))
	apiCmd.AddCommand(newAPILogsCmd(getManager))
	apiCmd.AddCommand(newAPIRunCmd(getManager, getConfig))

	return apiCmd
}
