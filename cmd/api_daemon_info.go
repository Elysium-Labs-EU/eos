package cmd

import (
	"errors"
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// apiDaemonInfoResult is the JSON shape returned by "eos api daemon info". Running and
// Pid are only meaningful for a standalone daemon — direct process state isn't
// observable for systemd/launchd/openrc, so they stay nil there (LogsHint still
// points callers at the right status command in every case).
type apiDaemonInfoResult struct {
	Version   *string `json:"version,omitempty"`
	Running   *bool   `json:"running,omitempty"`
	Pid       *int    `json:"pid,omitempty"`
	ManagedBy string  `json:"managed_by"`
	LogsHint  string  `json:"logs_hint"`
}

func newAPIDaemonInfoCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Return daemon status as JSON",
		Long: `Return the daemon's management backend, running state, and log access hint as JSON.

Output schema (stdout, JSON):
  {
    "managed_by": string        -- "standalone" | "systemd" | "launchd" | "openrc"
    "running":    bool|omitted  -- only known for a standalone daemon
    "pid":        int|omitted   -- only known for a standalone daemon
    "version":    string|omitted-- version of the running daemon binary, if resolvable
    "logs_hint":  string        -- command to run for log access
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error

Note: --all (listing every user's standalone daemon) is not supported in the API version.`,
		Example: `  eos api daemon info
  eos api daemon info | jq .running`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl, err := resolveAPIDaemonController(getConfig)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			return helpers.WriteJSON(cmd, ctrl.APIInfo(cmd))
		},
	}
}

// resolveAPIDaemonController fetches config and builds the DaemonController for the
// api daemon subcommands — the same construction the human-facing "eos daemon"
// command's PersistentPreRun performs.
func resolveAPIDaemonController(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) (DaemonController, error) {
	baseDir, cfg, identity, err := getConfig()
	if err != nil {
		return nil, fmt.Errorf("getting config: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("getting config: got nil config with no error")
	}
	ctrl, err := newDaemonController(cfg.Daemon, baseDir, &cfg.Health, cfg.Shutdown, cfg.Telemetry, cfg.UnderSystemd, identity)
	if err != nil {
		return nil, fmt.Errorf("resolving daemon mode: %w", err)
	}
	return ctrl, nil
}
