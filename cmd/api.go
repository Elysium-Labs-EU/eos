package cmd

import (
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
)

func newAPICmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, getDaemonConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	apiCmd := &cobra.Command{
		Use:           "api",
		Short:         "Machine-readable JSON interface",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	apiCmd.AddCommand(newAPIAddCmd(getManager))
	apiCmd.AddCommand(newAPIInfoCmd(getManager))
	apiCmd.AddCommand(newAPILogsCmd(getManager))
	apiCmd.AddCommand(newAPIRemoveCmd(getManager))
	apiCmd.AddCommand(newAPIRunCmd(getManager, getConfig))
	apiCmd.AddCommand(newAPIStatusCmd(getManager))
	apiCmd.AddCommand(newAPIStopCmd(getManager, getConfig))
	apiCmd.AddCommand(newAPIUpdateCmd(getManager))
	apiCmd.AddCommand(newAPIValidateCmd())
	apiCmd.AddCommand(newAPIDaemonCmd(getDaemonConfig))

	return apiCmd
}
