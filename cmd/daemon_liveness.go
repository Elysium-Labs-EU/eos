package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
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
		printDaemonDownBanner(cmd, &systemConfig.Daemon)
	}
}

// localMode is newManager's account of an in-process manager it built: which
// daemon that manager is working behind, and whether that daemon is answering.
// Both fields are zero whenever the invocation talks to a daemon over IPC, so
// the guards below stay quiet on every path that is already safe.
//
// It is resolved from the single socket probe newManager already makes to pick
// a manager, so the guards never dial again. A second dial would be a different
// observation than the one the manager choice was made on: a daemon coming up
// in between would make the guard fire and blame a --no-daemon the operator
// never passed.
type localMode struct {
	// LiveDaemonSocket is the socket of an eos daemon that answered while this
	// invocation was nonetheless wired to the in-process manager. Only
	// --no-daemon reaches that combination, so that flag is always the fix.
	LiveDaemonSocket string
	// SupervisorDown marks the opposite hazard: systemd, launchd or OpenRC owns
	// the daemon's lifecycle, but its unit is not answering, so nothing outside
	// this process would supervise a service started here.
	SupervisorDown bool
}

// localModeFn hands a command newManager's verdict. The indirection exists
// because commands are constructed before any config is loaded — see
// newRootCmd's lazyInit.
type localModeFn func() localMode

// daemonIsDown reports whether the daemon is confirmed offline. Every
// supervisor is probed the same way: by pinging the Unix socket under the
// ACTIVE base dir (baseDir/eos.sock), because that is the only signal scoped to
// the EOS_BASE_DIR this CLI actually targets. A DaemonConfig naming no
// supervisor at all is treated as "not confirmed down" so a banner never fires
// on an ambiguous signal.
//
// For systemd this deliberately does NOT use `systemctl is-active eos`: that
// check is host-global and reports "active" whenever ANY eos unit runs, even one
// supervising a different base dir. It would wrongly suppress the daemon-down
// warning for an alternate EOS_BASE_DIR whose daemon is not actually running,
// leaving a service pinned in 'starting' with no supervision (issue #12). A
// systemd unit serves exactly one base dir and listens on that base dir's socket,
// so the socket probe gives systemd the same per-base-dir isolation standalone
// already has. launchd and OpenRC carry that same base-dir socket (see
// config.LaunchdConfig.SocketPath) and so get the same probe rather than the
// old "never confirmed down" treatment.
func daemonIsDown(ctx context.Context, daemon *config.DaemonConfig) bool {
	endpoint, ok := config.ResolveDaemonEndpoint(*daemon)
	if !ok {
		return false
	}
	return !socketResponds(ctx, endpoint.SocketPath)
}

// refuseLocalWrite is the human-facing guard every state-changing command runs
// before its first write (stop, add, remove, update, system uninstall; run goes
// through refuseLocalStart, which also calls this). It refuses exactly the
// combination reload has always refused (see serviceReloader in cmd/reload.go):
// this process rewrites the state DB, the catalog rows and the service log
// files a running daemon owns, and signals PGIDs that daemon is supervising.
//
// The hazard is a daemon that is LIVE, not which supervisor started it, so
// standalone gets the same refusal systemd/launchd/OpenRC do. Standalone only
// reaches here under --no-daemon, and a standalone daemon answering on the
// socket is supervising real processes exactly like a unit-managed one.
func refuseLocalWrite(cmd *cobra.Command, mode localMode) error {
	if mode.LiveDaemonSocket == "" {
		return nil
	}

	cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "refusing to act in-process: an eos daemon is live on this base dir")
	cmd.PrintErrf(fmtIndentLabelTwoMsgLn, ui.TextMuted.Render("note:"), ui.TextBold.Render(mode.LiveDaemonSocket), ui.TextMuted.Render("is answering"))
	cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run without"), ui.TextCommand.Render("--no-daemon"), ui.TextMuted.Render("so the daemon handles this"))
	return helpers.ErrCommandFailed
}

// refuseLocalStart guards the one operation that spawns a process: everything
// refuseLocalWrite refuses, plus starting a service in-process while an
// external supervisor owns the daemon whose unit is currently down.
//
// That second case is the same orphaning bug narrowed to the outage window. The
// child is parented to the CLI, so it is reparented to init the moment the CLI
// exits and the pipes carrying its stdout and stderr lose their only reader;
// the daemon does not adopt it when the unit comes back, it SIGKILLs the
// recorded PGID (reconcileOrphans in internal/process/daemon.go) and starts the
// service again from the catalog. Refusing here keeps the operator from
// creating that state by accident, while --no-daemon stays an explicit opt-in
// to unsupervised local mode.
func refuseLocalStart(cmd *cobra.Command, mode localMode) error {
	if err := refuseLocalWrite(cmd, mode); err != nil {
		return err
	}
	if !mode.SupervisorDown {
		return nil
	}

	cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "refusing to start in-process: this host's eos daemon is supervisor-managed but its unit is not running")
	cmd.PrintErrf(fmtIndentLabelMsgLn, ui.TextMuted.Render("note:"), ui.TextMuted.Render("a service started now would be orphaned to init, with no health checks, metrics or log forwarding"))
	cmd.PrintErrf(fmtIndentLabelTwoMsgLn, ui.TextMuted.Render("start the daemon with:"), ui.TextCommand.Render(cmdnames.HintDaemonStart), ui.TextMuted.Render("then retry"))
	cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("or pass"), ui.TextCommand.Render("--no-daemon"), ui.TextMuted.Render("to run it unsupervised anyway"))
	return helpers.ErrCommandFailed
}

// apiRefuseLocalWrite is the machine-facing refuseLocalWrite: same refusal,
// written as the API commands' JSON error object so a script sees it on the
// contract it already parses. nil means proceed; the non-nil case is
// helpers.ErrAPICommandFailed, which the caller returns unchanged.
func apiRefuseLocalWrite(cmd *cobra.Command, mode localMode) error {
	if mode.LiveDaemonSocket == "" {
		return nil
	}
	return helpers.WriteJSONErr(cmd, fmt.Errorf(
		"refusing to act in-process: an eos daemon is live on %s; drop --no-daemon so the daemon handles this",
		mode.LiveDaemonSocket,
	))
}

// apiRefuseLocalStart is the machine-facing refuseLocalStart, plus a third
// refusal unique to the API contract. isLocal is true whenever the manager
// backing this invocation is the in-process LocalManager for any reason at
// all (SupervisorDown, a live-daemon conflict, or a plain unsupervised
// --no-daemon start) rather than a live daemon reached over IPC — mode
// alone can't tell that apart from "talking to the daemon", since both read
// as the same zero localMode{} (see localMode's doc comment).
//
// "eos api run" promises a pgid for a process that will still exist once
// the command exits; without a daemon nothing supervises it, so that
// promise can't be kept. Unlike "eos run" — which has a real interactive
// and scripted use blocking in the foreground to supervise a service itself
// — "eos api run" has no way to honor its own documented JSON-then-exit
// contract locally: blocking would hang every script piping its output
// through jq, and returning immediately would hand back a pgid for a
// process about to be orphaned. Refusing outright is the only response that
// doesn't hand a caller a false success.
func apiRefuseLocalStart(cmd *cobra.Command, mode localMode, isLocal bool) error {
	if err := apiRefuseLocalWrite(cmd, mode); err != nil {
		return err
	}
	if mode.SupervisorDown {
		return helpers.WriteJSONErr(cmd, fmt.Errorf(
			"refusing to start in-process: the eos daemon is supervisor-managed but its unit is not running, so this service would be orphaned to init; start the daemon with %q, or use \"eos run\" to supervise it in the foreground",
			cmdnames.HintDaemonStart,
		))
	}
	if !isLocal {
		return nil
	}
	return helpers.WriteJSONErr(cmd, errors.New(
		"refusing to start in-process: local mode has no daemon to supervise a service after this command exits; use \"eos run\" to supervise it in the foreground instead",
	))
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
// the outage before we decide whether to warn. Under an external supervisor
// getManager has no such path — it may only talk to the unit's socket or fall
// back to in-process work — so a stopped unit still reads as down here, which
// is exactly the case that produces a stuck 'starting' service.
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
	cmd.PrintErrf(fmtLabelMsgLn,
		ui.LabelWarning.Render("warning:"),
		"eos daemon is not running - service will stay in 'starting' with no health checks, metrics, or log forwarding",
	)
	cmd.PrintErrf(fmtIndentLabelMsg,
		ui.TextMuted.Render("start the daemon with:"),
		ui.TextCommand.Render(cmdnames.HintDaemonStart),
	)
}

// printDaemonDownBanner writes the daemon-down banner to stderr in the Rust-CLI
// house style: a bold severity label, then an aligned hint line naming the exact
// fix command. The ui styles collapse to plain text when stderr is not a TTY or
// NO_COLOR is set, so piped output stays clean.
//
// Standalone mode gets its own wording (issue #67): the very next call this
// process makes — getManager(), right after this banner — auto-starts a
// standalone daemon, so telling the operator to run "eos daemon start"
// themselves would be false; state below is still stale relative to the
// daemon that is about to come up, but no manual action is required.
func printDaemonDownBanner(cmd *cobra.Command, daemon *config.DaemonConfig) {
	if daemon != nil && daemon.Standalone != nil {
		cmd.PrintErrf(fmtLabelMsg,
			ui.LabelWarning.Render("warning:"),
			"eos daemon is not running - state below is last-known and may be stale; a standalone instance will be started automatically to serve this request",
		)
		return
	}
	cmd.PrintErrf(fmtLabelMsgLn,
		ui.LabelWarning.Render("warning:"),
		"eos daemon is not running - state below is last-known and may be stale",
	)
	cmd.PrintErrf(fmtIndentLabelMsg,
		ui.TextMuted.Render("start it with:"),
		ui.TextCommand.Render(cmdnames.HintDaemonStart),
	)
}

// warnCommandMightDivergeUnderDaemon tells the operator, before a local-mode
// start, that this shell's PATH resolving the command proves nothing about
// how the eos daemon would spawn it: PATH is the one thing local mode and a
// systemd-spawned daemon are guaranteed to differ on. It only fires for the
// in-process LocalManager — a live daemon reached over IPC already
// supervises the service with its own environment, so there is nothing to
// compare here. And it stays silent on anything validateCommandBinary itself
// would bail on (FirstCommandBinary returning ok=false), for the same
// conservative reason: naming the wrong binary is worse than naming none.
//
// A local start never has a live daemon to compare against: refuseLocalWrite
// already refuses any local write while one is reachable, so by the time
// this runs the daemon (if configured at all) is confirmed unreachable.
func warnCommandMightDivergeUnderDaemon(cmd *cobra.Command, mgr manager.ServiceManager, command string) {
	if _, ok := mgr.(*manager.LocalManager); !ok {
		return
	}
	binary, ok := manager.FirstCommandBinary(command)
	if !ok {
		return
	}
	if _, err := helpers.ResolveExecutable(binary); err != nil {
		return
	}
	cmd.PrintErrf(fmtLabelMsgLn,
		ui.LabelWarning.Render("warning:"),
		fmt.Sprintf("%q resolved using this shell's PATH; no eos daemon is reachable to confirm it would resolve the same way under one", binary),
	)
}

// runWarnCommandDivergence loads entry's service config just far enough to
// name its command's binary for warnCommandMightDivergeUnderDaemon. A load
// failure here is not reported: StartService reads the same file moments
// later and surfaces any real problem with it through the normal error path.
func runWarnCommandDivergence(cmd *cobra.Command, mgr manager.ServiceManager, entry *types.ServiceCatalogEntry) {
	cfg, err := manager.LoadServiceConfig(filepath.Join(entry.DirectoryPath, entry.ConfigFileName))
	if err != nil {
		return
	}
	warnCommandMightDivergeUnderDaemon(cmd, mgr, cfg.Command)
}
