package cmd

import (
	"github.com/spf13/cobra"

	"eos/internal/config"
	"eos/internal/manager"
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
