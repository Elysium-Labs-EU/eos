package cmd

import (
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type apiDaemonRemoveResult struct {
	Removed bool `json:"removed"`
}

func newAPIDaemonRemoveCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return newAPIDaemonRemoveCmdWithController(func() (DaemonController, error) { return newAPIDaemonController(getConfig) })
}

// newAPIDaemonRemoveCmdWithController takes the controller resolver directly so
// tests can inject a fakeDaemonController instead of a real config/process stack.
func newAPIDaemonRemoveCmdWithController(getCtrl func() (DaemonController, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove a stopped daemon; always outputs JSON",
		Long: `Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.

Output schema (stdout, JSON):
  {
    "removed": bool  -- true on success
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api daemon remove
  eos api daemon remove | jq .removed`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl, err := getCtrl()
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if err := ctrl.Remove(); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("removing daemon: %w", err))
			}
			return helpers.WriteJSON(cmd, apiDaemonRemoveResult{Removed: true})
		},
	}
}
