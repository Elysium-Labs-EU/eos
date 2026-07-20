package cmd

import (
	"context"
	"net"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

// warnIfDaemonDown prints a loud, last-known-state banner to stderr when the eos
// daemon is confirmed offline, so read commands (status, logs) never present a
// frozen DB as if the control plane were live.
//
// It resolves daemon config independently (the same pattern daemonIdentity uses)
// instead of going through getManager. That matters: in standalone mode
// getManager auto-starts the daemon, which would paper over the very outage we
// want to report — so liveness must be probed before any manager is built.
func warnIfDaemonDown(cmd *cobra.Command) {
	_, _, systemConfig, _, err := newSystemConfig()
	if err != nil || systemConfig == nil {
		return
	}
	if daemonIsDown(cmd.Context(), &systemConfig.Daemon) {
		printDaemonDownBanner(cmd)
	}
}

// daemonIsDown reports whether the daemon is confirmed offline for the two
// supervisors eos drives directly: systemd and standalone. Both are probed by
// pinging the Unix socket under the ACTIVE base dir (baseDir/eos.sock), because
// that is the only signal scoped to the EOS_BASE_DIR this CLI actually targets.
// Every other case — launchd, or no daemon configured — is treated as "not
// confirmed down" so a banner never fires on an ambiguous signal.
//
// For systemd this deliberately does NOT use `systemctl is-active eos`: that
// check is host-global and reports "active" whenever ANY eos unit runs, even one
// supervising a different base dir. It would wrongly suppress the daemon-down
// warning for an alternate EOS_BASE_DIR whose daemon is not actually running,
// leaving a service pinned in 'starting' with no supervision (issue #12). A
// systemd unit serves exactly one base dir and listens on that base dir's socket,
// so the socket probe gives systemd the same per-base-dir isolation standalone
// already has.
func daemonIsDown(ctx context.Context, daemon *config.DaemonConfig) bool {
	switch {
	case daemon.Systemd != nil:
		return !socketResponds(ctx, daemon.Systemd.SocketPath)
	case daemon.Standalone != nil:
		return !socketResponds(ctx, daemon.Standalone.SocketPath)
	default:
		return false
	}
}

// socketResponds reports whether the daemon's Unix socket accepts a connection.
// A refused dial (dead daemon, whether or not it left a stale socket file behind)
// reads as down.
func socketResponds(ctx context.Context, socketPath string) bool {
	dialCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "unix", socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// warnDaemonDownBeforeStart prints a start-specific banner when the daemon is
// confirmed offline, so `eos run` never silently leaves a service pinned in
// 'starting'. Unlike warnIfDaemonDown it takes the already-resolved daemon
// config and must be called AFTER getManager: in standalone mode getManager
// auto-starts the daemon, so probing afterwards lets that self-heal path clear
// the outage before we decide whether to warn. In systemd mode getManager
// builds a local manager and never touches the separate daemon unit, so a down
// unit still reads as down here — exactly the case that produces a stuck
// 'starting' service.
func warnDaemonDownBeforeStart(cmd *cobra.Command, daemon *config.DaemonConfig) {
	if daemon == nil {
		return
	}
	if daemonIsDown(cmd.Context(), daemon) {
		printDaemonDownStartWarning(cmd)
	}
}

// printDaemonDownStartWarning writes the run-time daemon-down banner to stderr.
// It spells out the concrete consequence — the service stays in 'starting' with
// no health checks, metrics, or log forwarding — and names the fix command, so
// the operator knows the start "succeeded" but is unsupervised.
func printDaemonDownStartWarning(cmd *cobra.Command) {
	cmd.PrintErrf("%s %s\n",
		ui.LabelWarning.Render("warning:"),
		"eos daemon is not running - service will stay in 'starting' with no health checks, metrics, or log forwarding",
	)
	cmd.PrintErrf("  %s %s\n\n",
		ui.TextMuted.Render("start the daemon with:"),
		ui.TextCommand.Render("eos daemon start"),
	)
}

// printDaemonDownBanner writes the daemon-down banner to stderr in the Rust-CLI
// house style: a bold severity label, then an aligned hint line naming the exact
// fix command. The ui styles collapse to plain text when stderr is not a TTY or
// NO_COLOR is set, so piped output stays clean.
func printDaemonDownBanner(cmd *cobra.Command) {
	cmd.PrintErrf("%s %s\n",
		ui.LabelWarning.Render("warning:"),
		"eos daemon is not running - state below is last-known and may be stale",
	)
	cmd.PrintErrf("  %s %s\n\n",
		ui.TextMuted.Render("start it with:"),
		ui.TextCommand.Render("eos daemon start"),
	)
}
