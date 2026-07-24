package cmd

import (
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonStopResult struct {
	Stopped bool `json:"stopped"`
}

func newAPIDaemonStopCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return newAPIDaemonStopCmdWithController(func() (DaemonController, error) { return newAPIDaemonController(getConfig) })
}

// newAPIDaemonStopCmdWithController takes the controller resolver directly so
// tests can inject a fakeDaemonController instead of a real config/process stack.
func newAPIDaemonStopCmdWithController(getCtrl func() (DaemonController, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon; always outputs JSON",
		Long: `Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.

Output schema (stdout, JSON):
  {
    "stopped": bool  -- true if a running daemon was stopped, false if it was not running
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api daemon stop
  eos api daemon stop | jq .stopped`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl, err := getCtrl()
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			stopped, err := ctrl.Stop(cmd.Context(), cmd, false)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("stopping daemon: %w", err))
			}
			return helpers.WriteJSON(cmd, apiDaemonStopResult{Stopped: stopped})
		},
	}
}
