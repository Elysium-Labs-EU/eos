package cmd

import (
	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonStopResult struct {
	Stopped bool `json:"stopped"`
}

func newAPIDaemonStopCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon; always outputs JSON",
		Long: `Stop the running daemon process. If managed by systemd, delegates to systemctl stop
(requires root); if managed by OpenRC, delegates to rc-service stop (requires root).
Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.

Output schema (stdout, JSON):
  { "stopped": bool }  -- false if the daemon was not running

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
			ctrl, err := resolveAPIDaemonController(getConfig)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			stopped, err := ctrl.Stop(cmd.Context(), cmd, false)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			return helpers.WriteJSON(cmd, apiDaemonStopResult{Stopped: stopped})
		},
	}
}
