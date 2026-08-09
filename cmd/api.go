package cmd

import (
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

func newAPICmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, getDaemonConfig func() (string, *config.SystemConfig, userutil.Identity, error), managerMode localModeFn) *cobra.Command {
	apiCmd := &cobra.Command{
		Use:           cmdnames.API,
		Short:         "Machine-readable JSON interface",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	apiCmd.AddCommand(newAPIAddCmd(getManager, managerMode))
	apiCmd.AddCommand(newAPIInfoCmd(getManager))
	apiCmd.AddCommand(newAPILogsCmd(getManager))
	apiCmd.AddCommand(newAPIRemoveCmd(getManager, managerMode))
	apiCmd.AddCommand(newAPIRunCmd(getManager, getConfig, managerMode))
	apiCmd.AddCommand(newAPIStatusCmd(getManager))
	apiCmd.AddCommand(newAPIStopCmd(getManager, getConfig, managerMode))
	apiCmd.AddCommand(newAPIUpdateCmd(getManager, managerMode))
	apiCmd.AddCommand(newAPIValidateCmd())
	apiCmd.AddCommand(newAPIDaemonCmd(getDaemonConfig))

	return apiCmd
}
