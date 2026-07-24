package cmd

import (
	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonStartResult struct {
	Started bool `json:"started"`
}

func newAPIDaemonStartCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon; always outputs JSON",
		Long: `Start the daemon process.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).
If an OpenRC init script is installed, delegates to "rc-service eos start" (requires root).
Otherwise, starts the daemon detached in the background by default; control returns once
the PID file is written (timeout: 5s). Pass --foreground to block until the daemon exits
instead — not useful for scripting, but kept for parity with 'eos daemon start'.

Output schema (stdout, JSON):
  { "started": bool }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api daemon start
  eos api daemon start | jq .started`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl, err := resolveAPIDaemonController(getConfig)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if err := ctrl.Start(cmd.Context(), !foreground, false, false); err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			return helpers.WriteJSON(cmd, apiDaemonStartResult{Started: true})
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "block until the daemon exits instead of starting detached")
	return cmd
}
