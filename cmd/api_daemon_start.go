package cmd

import (
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
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

Idempotent: if the daemon is already running, returns "started": false with exit code 0 instead of erroring, matching "eos api daemon stop"'s idempotency contract.

Output schema (stdout, JSON):
  {
    "started": bool  -- true on success, false if the daemon was already running
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
			if ctrl.IsRunning(cmd.Context()) {
				return helpers.WriteJSON(cmd, apiDaemonStartResult{Started: false})
			}
			if err := ctrl.Start(cmd.Context(), true, false, false); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("starting daemon: %w", err))
			}
			return helpers.WriteJSON(cmd, apiDaemonStartResult{Started: true})
		},
	}
}
