package cmd

import (
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

func newAPICmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	apiCmd := &cobra.Command{
		Use:           "api",
		Short:         "Machine-readable JSON interface",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	apiCmd.AddCommand(newAPIRunCmd(getManager, getConfig))
	apiCmd.AddCommand(newAPILogsCmd(getManager))

	return apiCmd
}
