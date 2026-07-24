package cmd

import (
	"fmt"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonStartResult struct {
	Started bool `json:"started"`
}

func newAPIDaemonStartCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return newAPIDaemonStartCmdWithController(func() (DaemonController, error) { return newAPIDaemonController(getConfig) })
}

// newAPIDaemonStartCmdWithController takes the controller resolver directly so
// tests can inject a fakeDaemonController instead of a real config/process stack.
func newAPIDaemonStartCmdWithController(getCtrl func() (DaemonController, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon; always outputs JSON",
		Long: `Start the daemon process.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).
Otherwise starts the daemon detached in the background; control returns once the PID file and socket are confirmed live (timeout: 5s). Unlike "eos daemon start", there is no --foreground option — the JSON contract requires the command to return once startup is confirmed, not block for the daemon's lifetime.

Output schema (stdout, JSON):
  {
    "started": bool  -- true on success
  }

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
			ctrl, err := getCtrl()
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if err := ctrl.Start(cmd.Context(), true, false, false); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("starting daemon: %w", err))
			}
			return helpers.WriteJSON(cmd, apiDaemonStartResult{Started: true})
		},
	}
}
