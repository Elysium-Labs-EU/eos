package cmd

import (
	"context"
	"net"
	"strings"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
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
// supervisors eos drives directly: systemd (authoritative "is-active" check) and
// standalone (Unix-socket ping). Every other case — launchd, or no daemon
// configured — is treated as "not confirmed down" so a banner never fires on an
// ambiguous signal.
func daemonIsDown(ctx context.Context, daemon *config.DaemonConfig) bool {
	switch {
	case daemon.Systemd != nil:
		return !systemdUnitActive(ctx, daemon.Systemd.UserUnit)
	case daemon.Standalone != nil:
		return !socketResponds(ctx, daemon.Standalone.SocketPath)
	default:
		return false
	}
}

// systemdUnitActive shells out to `systemctl is-active eos`, reusing the same
// systemctlArgs plumbing as the daemon controller. Only a literal "active"
// counts as live; "inactive", "failed", missing systemctl, etc. all read as down.
func systemdUnitActive(ctx context.Context, userUnit bool) bool {
	out, _ := execRunCmd(ctx, "systemctl", systemctlArgs(userUnit, "is-active", "eos")...)
	return strings.TrimSpace(string(out)) == "active"
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
