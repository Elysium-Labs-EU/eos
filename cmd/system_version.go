package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/process"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

// sysRunInfo backs the "system info" subcommand's RunE.
func sysRunInfo(cmd *cobra.Command, systemCmd *cobra.Command) error {
	installDir, baseDir, cfg, _, err := newSystemConfig()
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
		return helpers.ErrCommandFailed
	}
	infoCmd(cmd, installDir, baseDir, cfg)
	return nil
}

// sysRunVersion backs the "system version" subcommand's RunE. Version drift
// is a bonus, not worth failing the command over: if the config can't be
// resolved, the drift check is just skipped.
func sysRunVersion(cmd *cobra.Command) {
	cmd.Println(buildinfo.Get())
	if _, _, systemConfig, _, err := newSystemConfig(); err == nil {
		printVersionDrift(cmd, systemConfig.Daemon)
	}
}

func infoCmd(cmd *cobra.Command, installDir string, baseDir string, config *config.SystemConfig) {
	cmd.Println()
	cmd.Printf(fmtHeading, ui.TextBold.Render("System Config"))
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("install dir:"), installDir)
	cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("base dir:"), baseDir)
	cmd.Printf(fmtHeading, ui.TextBold.Render("Daemon"))
	printDaemonInfo(cmd, config.Daemon)
	cmd.Printf(fmtHeading, ui.TextBold.Render("Health Check"))
	cmd.Printf(fmtIndentLabelAnyLn, ui.TextMuted.Render("timeout enabled:"), config.Health.Timeout.Enable)
	if config.Health.Timeout.Enable {
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit)
	} else {
		cmd.Printf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit, ui.TextMuted.Render("(not active)"))
	}
	cmd.Printf(fmtHeading, ui.TextBold.Render("Shutdown"))
	cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("grace period:"), config.Shutdown.GracePeriod)
	cmd.Printf(fmtHeading, ui.TextBold.Render("Telemetry"))
	cmd.Printf(fmtIndentLabelAnyLn, ui.TextMuted.Render("enabled:"), config.Telemetry.Enable)
	if config.Telemetry.Enable {
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("endpoint:"), config.Telemetry.Endpoint)
		cmd.Printf(fmtIndentLabelAnyLn, ui.TextMuted.Render("insecure:"), config.Telemetry.Insecure)
	}
	cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("hint:"), fmt.Sprintf("sinks, telemetry, and health thresholds as configured in config.yaml: %s", ui.TextCommand.Render(cmdnames.HintConfigShow)))
}

// printDaemonInfo renders the Daemon block of "eos system info" for whichever
// supervisor is active. Every mode prints the socket the CLI talks to: the
// block covered standalone and systemd only, so a launchd- or OpenRC-managed
// install hit the systemd branch and dereferenced a nil SystemdConfig, and no
// mode but standalone ever showed a socket at all.
func printDaemonInfo(cmd *cobra.Command, daemon config.DaemonConfig) {
	switch {
	case daemon.Standalone != nil:
		printStandaloneStaleWarning(cmd, daemon.Standalone)
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("systemd managed:"), false)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("pid file:"), daemon.Standalone.PIDFile)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket:"), daemon.Standalone.SocketPath)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket timeout:"), daemon.Standalone.SocketTimeout)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("log dir:"), daemon.Standalone.Log.LogDir)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("log file:"), daemon.Standalone.Log.LogFileName)
		cmd.Printf("  %s %d\n", ui.TextMuted.Render("log max files:"), daemon.Standalone.Log.LogMaxFiles)
		cmd.Printf("  %s %d\n\n", ui.TextMuted.Render("log size limit:"), daemon.Standalone.Log.LogFileSizeLimit)
	case daemon.Systemd != nil:
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("systemd managed:"), true)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("systemd target directory:"), daemon.Systemd.SystemdTargetDir)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("systemd target filename:"), daemon.Systemd.SystemdTargetFileName)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket:"), daemon.Systemd.SocketPath)
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("user unit:"), daemon.Systemd.UserUnit)
	case daemon.Launchd != nil:
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("launchd managed:"), true)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("launchd target directory:"), daemon.Launchd.LaunchdTargetDir)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("launchd plist filename:"), daemon.Launchd.LaunchdPlistFileName)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket:"), daemon.Launchd.SocketPath)
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("user agent:"), daemon.Launchd.UserAgent)
	case daemon.OpenRC != nil:
		cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("openrc managed:"), true)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("openrc init directory:"), daemon.OpenRC.InitDir)
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("openrc init filename:"), daemon.OpenRC.InitFileName)
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("socket:"), daemon.OpenRC.SocketPath)
	default:
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("supervisor:"), "(none configured)")
	}
}

// printStandaloneStaleWarning warns when a running standalone daemon is still
// backed by a since-replaced binary. Unlike "eos daemon info", this Daemon
// block otherwise prints static config only — no liveness check at all — so
// without this it was the one remaining place an operator could look straight
// at a stale daemon and see nothing wrong. Reuses staleBinary, the same
// process.RunningExeInode/CurrentExecutableInode comparison "eos daemon info"
// and "eos daemon info --all" already rely on, rather than deriving the fact
// a third way.
func printStandaloneStaleWarning(cmd *cobra.Command, cfg *config.StandaloneDaemonConfig) {
	status, err := process.StatusStandaloneDaemon(cfg)
	if err != nil || !status.Running {
		return
	}
	if staleBinary(*status.Pid, process.CurrentExecutableInode()) {
		cmd.Printf(fmtLabelMsgLn, ui.LabelWarning.Render("⚠"), fmt.Sprintf("daemon is running (pid %d) — on a since-replaced binary, restart needed", *status.Pid))
	}
}

// resolveDaemonVersion queries the version of the actual running daemon
// process, dispatching to the supervisor-specific strategy.
func resolveDaemonVersion(ctx context.Context, daemon config.DaemonConfig) (string, error) {
	switch {
	case daemon.Standalone != nil:
		return resolveStandaloneDaemonVersion(ctx, daemon.Standalone)
	case daemon.Systemd != nil:
		return resolveSystemdDaemonVersion(ctx, daemon.Systemd)
	default:
		return "", errors.New("daemon version check not supported for this supervisor")
	}
}

// resolveStandaloneDaemonVersion checks liveness first rather than going
// straight through manager.NewDaemonManager, which auto-starts a standalone
// daemon that isn't running — a version check has no business spinning one up.
func resolveStandaloneDaemonVersion(ctx context.Context, cfg *config.StandaloneDaemonConfig) (string, error) {
	status, err := process.StatusStandaloneDaemon(cfg)
	if err != nil {
		return "", fmt.Errorf("checking daemon status: %w", err)
	}
	if !status.Running {
		return "", errors.New("daemon not running")
	}

	dm, err := manager.NewDaemonManager(ctx, cfg.SocketPath, cfg.PIDFile, cfg.SocketTimeout, false)
	if err != nil {
		return "", fmt.Errorf("connecting to daemon: %w", err)
	}
	version, err := dm.GetVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("querying daemon version: %w", err)
	}
	return version.Version, nil
}

// resolveSystemdDaemonVersion follows /proc/<pid>/exe to the actual running
// binary and asks it directly — the same drift systemdDaemonRunningVersion
// already reports in 'eos daemon info'. That works whether or not the unit's
// socket is currently answering, which the IPC path above requires.
func resolveSystemdDaemonVersion(ctx context.Context, cfg *config.SystemdConfig) (string, error) {
	full, err := systemdDaemonRunningVersion(ctx, cfg.UserUnit)
	if err != nil {
		return "", err
	}
	return firstVersionToken(full)
}

// firstVersionToken extracts the version number from buildinfo.Get()'s
// "vX.Y.Z (commit: ..., built: ...)" format. Pure — no I/O.
func firstVersionToken(full string) (string, error) {
	fields := strings.Fields(full)
	if len(fields) == 0 {
		return "", errors.New("empty version output")
	}
	return fields[0], nil
}

// resolveLatestVersion fetches the latest published release tag. It returns
// ("", nil) when the current version is already latest or ahead — an empty
// string signals "nothing to report", distinct from a lookup failure.
func resolveLatestVersion(ctx context.Context, currentVersion string) (string, error) {
	if currentVersion == "dev" {
		return "", errors.New("dev build: no release to compare against")
	}
	release, err := fetchLatestRelease(ctx, false)
	if err != nil {
		return "", err
	}
	if semver.Compare(currentVersion, release.TagName) >= 0 {
		return "", nil
	}
	return release.TagName, nil
}

// versionDriftInfo is the pure comparison data printVersionDrift renders —
// split out from the printing so the "what counts as drift, what to print"
// branching can be table-tested without a cobra.Command or network access.
type versionDriftInfo struct {
	cliVersion    string
	daemonVersion string
	latestVersion string // "" when no newer release was found
	daemonKnown   bool
}

// gatherVersionDrift runs both lookups. Each is best-effort: an unreachable
// daemon or an offline GitHub check are normal states, not failures worth
// surfacing, so a failed lookup just leaves its field at its zero value.
func gatherVersionDrift(ctx context.Context, daemon config.DaemonConfig) versionDriftInfo {
	cliVersion := buildinfo.GetVersionOnly()
	info := versionDriftInfo{cliVersion: cliVersion}

	if daemonVersion, err := resolveDaemonVersion(ctx, daemon); err == nil {
		info.daemonVersion = daemonVersion
		info.daemonKnown = true
	}
	if latestVersion, err := resolveLatestVersion(ctx, cliVersion); err == nil {
		info.latestVersion = latestVersion
	}
	return info
}

func (v versionDriftInfo) daemonDrift() bool {
	return v.daemonKnown && v.daemonVersion != v.cliVersion
}

func (v versionDriftInfo) latestDrift() bool {
	return v.latestVersion != ""
}

// versionDriftLines renders the drift diff and its suggested fix as one line
// per element (nil when nothing diverges) — pure, so it's table-tested
// directly instead of through cmd output.
func versionDriftLines(v versionDriftInfo) []string {
	if !v.daemonDrift() && !v.latestDrift() {
		return nil
	}

	lines := []string{
		"",
		ui.TextBold.Render("Version drift detected"),
		"",
		fmt.Sprintf("  %s %s", ui.TextMuted.Render("CLI:"), v.cliVersion),
	}
	if v.daemonKnown {
		lines = append(lines, fmt.Sprintf("  %s %s", ui.TextMuted.Render("Daemon:"), v.daemonVersion))
	}
	if v.latestDrift() {
		lines = append(lines, fmt.Sprintf("  %s %s", ui.TextMuted.Render("Latest:"), v.latestVersion))
	}
	lines = append(lines, "")

	if v.latestDrift() {
		lines = append(lines, fmt.Sprintf("  %s %s", ui.TextMuted.Render("run:"), ui.TextCommand.Render(cmdnames.HintSystemUpdate)))
	}
	if v.daemonDrift() {
		lines = append(lines, fmt.Sprintf("  %s %s", ui.TextMuted.Render("note:"), "the running daemon won't pick up a binary update until it's restarted (eos daemon stop && eos daemon start); as root, 'eos system update' only restarts the invoking user's daemon, so other users' daemons stay stale"))
	}
	lines = append(lines, "")
	return lines
}

// printVersionDrift compares the CLI's own version against the running
// daemon's and the latest published release, printing a diff and the fix only
// when they actually disagree.
func printVersionDrift(cmd *cobra.Command, daemon config.DaemonConfig) {
	for _, line := range versionDriftLines(gatherVersionDrift(cmd.Context(), daemon)) {
		cmd.Println(line)
	}
}
