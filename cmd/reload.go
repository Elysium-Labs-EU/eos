package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

// serviceReloader is the daemon-backed capability the reload command needs. Only
// DaemonManager implements it: reload launches a second instance and health-gates
// the cutover inside the supervising daemon, where the service processes live.
// An in-process (--no-daemon) manager would spawn the new instance as a child of
// the short-lived CLI, so reload deliberately requires the daemon and errors out
// otherwise rather than pretending to swap.
type serviceReloader interface {
	ReloadService(ctx context.Context, name string, cfg manager.ReloadConfig) (manager.ReloadResult, error)
}

const (
	// reloadTickerPeriod is how often the drain of the old instance polls for
	// exit; matches the stop/restart cadence.
	reloadTickerPeriod = 200 * time.Millisecond
	// reloadReadinessTimeout bounds how long to wait for the new instance to
	// pass the health check before giving up and keeping the old one.
	reloadReadinessTimeout = 30 * time.Second
	// reloadProbeInterval is how often the readiness probe runs while waiting.
	reloadProbeInterval = 500 * time.Millisecond
)

func newReloadCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdnames.UseReload,
		Short: "Zero-downtime reload of a service",
		Long: `Reload a running service without dropping connections.

eos starts a fresh instance alongside the running one, waits for the new
instance to pass its health check, and only then drains the old instance. If the
new instance never becomes healthy the old one keeps serving, so a broken deploy
is a no-op rather than an outage.

Unlike restart (stop-then-start, which drops the listening socket in between),
reload overlaps the two instances. That overlap only works if the service shares
its listening socket: the service MUST bind its port with SO_REUSEPORT so both
the old and new instance can listen on the same address at once, and it must bind
promptly on startup so the new instance is accepting before the old one is
drained. eos does not own the socket or proxy traffic; it only sequences the
cutover. A service that binds without SO_REUSEPORT will fail to start its second
instance (address already in use) and the reload will abort with the old
instance untouched.`,
		Example:           `  eos reload cms    # start a new instance, health-check it, then drain the old one`,
		Args:              cobra.ExactArgs(1),
		SilenceUsage:      true,
		SilenceErrors:     true,
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()
			cfg := getConfig()

			cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "reloading", ui.TextBold.Render(serviceName))

			exists, err := mgr.IsServiceRegistered(cmd.Context(), serviceName)
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
				return helpers.ErrCommandFailed
			}
			if !exists {
				cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
				cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(cmdnames.HintAdd), ui.TextMuted.Render("to register it"))
				return helpers.ErrCommandFailed
			}

			reloader, ok := mgr.(serviceReloader)
			if !ok {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "reload requires the standalone eos daemon")
				cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run without"), ui.TextCommand.Render("--no-daemon"), ui.TextMuted.Render("to use the daemon"))
				return helpers.ErrCommandFailed
			}

			result, err := reloader.ReloadService(cmd.Context(), serviceName, manager.ReloadConfig{
				GracePeriod:      cfg.Shutdown.GracePeriod,
				TickerPeriod:     reloadTickerPeriod,
				ReadinessTimeout: reloadReadinessTimeout,
				ProbeInterval:    reloadProbeInterval,
			})
			if err != nil {
				if errors.Is(err, manager.ErrServiceNotRunning) {
					cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not running")
					cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintRun, serviceName)), ui.TextMuted.Render("to start it"))
					return helpers.ErrCommandFailed
				}
				if errors.Is(err, manager.ErrReloadNotReady) {
					cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("new instance never became healthy: %v", err))
					cmd.PrintErrf(fmtIndentLabelTwoMsgLn, ui.TextMuted.Render("note:"), ui.TextBold.Render(serviceName), "kept the old instance running")
					cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintLogs, serviceName)), ui.TextMuted.Render("to see why it failed"))
					return helpers.ErrCommandFailed
				}
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("reloading service: %v", err))
				return helpers.ErrCommandFailed
			}

			printReloadSuccessOutput(cmd, serviceName, result)
			return nil
		},
	}

	return cmd
}

func printReloadSuccessOutput(cmd *cobra.Command, serviceName string, result manager.ReloadResult) {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("reloaded (PGID %d to %d)", result.OldPGID, result.NewPGID))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintInfo, serviceName)), ui.TextMuted.Render("to view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintLogs, serviceName)), ui.TextMuted.Render("to view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render(cmdnames.HintStatus))
}
