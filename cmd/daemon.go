package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/process"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type DaemonController interface {
	Start(ctx context.Context, detach bool, logToFileAndConsole bool, verbose bool) error
	Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error)
	// IsRunning reports whether the daemon is currently running, without side
	// effects — used to gate restart prompts before Stop() is ever called.
	IsRunning(ctx context.Context) bool
	Remove() error
	Info(cmd *cobra.Command)
	Logs(cmd *cobra.Command, lines int, follow bool)
	LogsHint() string
}

type standaloneDaemonController struct {
	baseDir      string
	identity     userutil.Identity
	telemetry    config.TelemetryConfig
	cfg          config.StandaloneDaemonConfig
	health       config.HealthConfig
	shutdown     config.ShutdownConfig
	underSystemd bool
}

func (c *standaloneDaemonController) Start(ctx context.Context, detach bool, logToFileAndConsole bool, verbose bool) error {
	if detach && !c.underSystemd {
		return forkDaemon(ctx, &c.cfg, verbose, c.identity)
	}
	return process.StartStandaloneDaemon(ctx, process.StandaloneDaemonStartOptions{
		BaseDir:             c.baseDir,
		LogToFileAndConsole: logToFileAndConsole,
		Verbose:             verbose,
		UnderSystemd:        c.underSystemd,
	}, &c.cfg, &c.health, c.shutdown, c.telemetry)
}

func (c *standaloneDaemonController) Stop(_ context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	helpers.Debugf(cmd, verbose, "reading pid file: %s", c.cfg.PIDFile)
	killed, err := process.StopStandaloneDaemon(c.cfg.PIDFile, c.cfg.SocketPath)
	if err != nil {
		helpers.Debugf(cmd, verbose, "stop failed: %v", err)
		return killed, err
	}
	if killed {
		helpers.Debugf(cmd, verbose, "sent termination signal, removing socket: %s", c.cfg.SocketPath)
	}
	return killed, nil
}

// IsRunning checks the PID file/process directly, the same primitive Remove()
// uses to refuse acting on a live daemon. A status-check error is treated as
// "running" so callers fall back to the old prompt-first behavior rather than
// silently skipping a restart we couldn't actually confirm was unnecessary.
func (c *standaloneDaemonController) IsRunning(_ context.Context) bool {
	status, err := process.StatusStandaloneDaemon(&c.cfg)
	if err != nil {
		return true
	}
	return status.Running
}

func (c *standaloneDaemonController) Remove() error {
	status, err := process.StatusStandaloneDaemon(&c.cfg)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if status.Running {
		return errors.New("daemon is running — stop it first with 'eos daemon stop'")
	}
	_, err = process.RemoveStandaloneDaemon(&c.cfg)
	return err
}

func (c *standaloneDaemonController) Info(cmd *cobra.Command) {
	status, err := process.StatusStandaloneDaemon(&c.cfg)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting daemon info: %v", err))
		return
	}
	if !status.Running {
		if status.Pid != nil {
			cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon found but not running"))
			printStandaloneDaemonDetails(cmd, *status.Pid, &c.cfg)
			return
		}
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon is not running"))
		return
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelSuccess.Render("✓"), ui.TextBold.Render("daemon is running"))
	if version, err := resolveStandaloneDaemonVersion(cmd.Context(), &c.cfg); err == nil {
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("running version:"), version)
	}
	cmd.Println()
	printStandaloneDaemonDetails(cmd, *status.Pid, &c.cfg)
}

func (c *standaloneDaemonController) LogsHint() string {
	return eosDaemonLogsCmdName
}

func (c *standaloneDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	tailDaemonLogFile(cmd, c.baseDir, c.cfg.Log.LogFileName, lines, follow)
}

// tailDaemonLogFile tails the daemon's own rotated log file. The daemon writes and
// rotates this file itself regardless of supervisor (standalone, systemd, or launchd),
// so both standaloneDaemonController and launchdDaemonController share this — launchd
// has no persistent unified log like journald, so reusing the daemon's own log file is
// the correct equivalent, not a workaround.
func tailDaemonLogFile(cmd *cobra.Command, baseDir string, logFileName string, lines int, follow bool) {
	logPath := filepath.Join(manager.CreateLogDirPath(baseDir), logFileName)

	if _, err := os.Stat(logPath); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting log file: %v", err))
		return
	}
	if lines < 0 || lines > 10000 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
		return
	}

	if follow {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "streaming daemon logs")
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "showing daemon logs")
	}

	tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
	if follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, logPath)

	tailPath, err := helpers.ResolveExecutable("tail")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving tail: %v", err))
		return
	}
	// #nosec G204 - args are validated above, tailPath is resolved via LookPath
	tailCmd := exec.CommandContext(cmd.Context(), tailPath, tailArgs...)
	tailCmd.Stderr = cmd.ErrOrStderr()

	stdout, pipeErr := tailCmd.StdoutPipe()
	if pipeErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating log pipe: %v", pipeErr))
		return
	}
	if err := tailCmd.Start(); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting log command: %v", err))
		return
	}
	renderServiceLogs(cmd.OutOrStdout(), stdout, "")
	if err := tailCmd.Wait(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if exitErr.ExitCode() != 130 { // 130 = Ctrl+C
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("log command failed: %v", err))
			}
		}
	}
}

type systemdDaemonController struct {
	// checkDir is isAccessibleDir in production (set by newDaemonController); tests inject a
	// fake so Stop's user-bus check doesn't depend on what runtime dirs genuinely exist in the
	// environment running the test (e.g. a root-run CI job where /run/user/0 is real).
	checkDir dirAccessCheckFn
	cfg      config.SystemdConfig
}

func (c systemdDaemonController) Start(ctx context.Context, _ bool, _ bool, _ bool) error {
	if !c.cfg.UserUnit && os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	systemctlPath, err := helpers.ResolveExecutable("systemctl")
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, systemctlPath, systemctlArgs(c.cfg.UserUnit, "start", "eos")...).CombinedOutput() // #nosec G204 -- args are a fixed set built from a bool, not external input; systemctlPath resolved via LookPath
	if err != nil {
		return fmt.Errorf("starting systemd service: %s", out)
	}
	return nil
}

func (c systemdDaemonController) Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	args := systemctlArgs(c.cfg.UserUnit, "stop", "eos")
	scope := "system"
	if c.cfg.UserUnit {
		scope = "user"
	}
	helpers.Debugf(cmd, verbose, "resolved scope: %s (unit dir: %s)", scope, c.cfg.SystemdTargetDir)

	if c.cfg.UserUnit {
		effectiveUser, effectiveUserErr := userutil.EffectiveUser()
		if effectiveUserErr != nil {
			return false, fmt.Errorf("getting current user: %w", effectiveUserErr)
		}
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			return false, fmt.Errorf("getting current user credentials: %w", credErr)
		}
		if err := ensureUserBusAvailable(ctx, cmd, verbose, effectiveUser.Username, int(effectiveUID), userRuntimeDir(int(effectiveUID)), execRunCmd, c.checkDir); err != nil {
			return false, fmt.Errorf("preparing user bus: %w", err)
		}
	}

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(args, " "))
	systemctlPath, err := helpers.ResolveExecutable("systemctl")
	if err != nil {
		return false, err
	}
	out, err := exec.CommandContext(ctx, systemctlPath, args...).CombinedOutput() // #nosec G204 -- args are a fixed set built from a bool, not external input; systemctlPath resolved via LookPath
	if err != nil {
		helpers.Debugf(cmd, verbose, "systemctl exited with error: %s", strings.TrimSpace(string(out)))
		return false, fmt.Errorf("stopping systemd service: %s", out)
	}
	helpers.Debugf(cmd, verbose, "systemctl stop succeeded")
	return true, nil
}

// IsRunning probes the base-dir-scoped socket the same way daemonIsDown() does,
// not `systemctl is-active` — that check is host-global and would say "running"
// for a unit supervising a different EOS_BASE_DIR entirely (issue #12).
func (c systemdDaemonController) IsRunning(ctx context.Context) bool {
	return socketResponds(ctx, c.cfg.SocketPath)
}

func (c systemdDaemonController) Remove() error {
	return os.Remove(c.cfg.SystemdTargetDir + c.cfg.SystemdTargetFileName)
}

func (c systemdDaemonController) Info(cmd *cobra.Command) {
	printSystemdDaemonDetails(cmd, c.cfg)
}

func (c systemdDaemonController) LogsHint() string {
	if c.cfg.UserUnit {
		return "journalctl --user -u eos -f"
	}
	return "journalctl -u eos -f"
}

// buildJournalArgs assembles the journalctl arguments for the daemon unit,
// scoped to the user bus when running a user unit.
func buildJournalArgs(userUnit bool, lines int, follow bool) []string {
	journalArgs := systemctlArgs(userUnit, "-u", "eos", "-n", fmt.Sprintf("%d", lines))
	if follow {
		journalArgs = append(journalArgs, "-f")
	}
	return journalArgs
}

// reportJournalExit prints a failure only for real journalctl errors, treating
// exit code 130 (SIGINT from the user's Ctrl-C while following) as normal.
func reportJournalExit(cmd *cobra.Command, err error) {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() != 130 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("journalctl failed: %v", err))
	}
}

// runJournalStream runs journalctl with the given args, forwarding its output to
// the command's streams.
func runJournalStream(cmd *cobra.Command, journalArgs []string) {
	journalctlPath, err := helpers.ResolveExecutable("journalctl")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving journalctl: %v", err))
		return
	}
	// #nosec G204 - journalArgs contains only --user, -u, eos, -n, <int>, and optionally -f; journalctlPath resolved via LookPath
	journalCmd := exec.CommandContext(cmd.Context(), journalctlPath, journalArgs...)
	journalCmd.Stdout = cmd.OutOrStdout()
	journalCmd.Stderr = cmd.ErrOrStderr()
	if err := journalCmd.Start(); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting journalctl: %v", err))
		return
	}
	if err := journalCmd.Wait(); err != nil {
		reportJournalExit(cmd, err)
	}
}

func (c systemdDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	if lines < 0 || lines > 10000 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
		return
	}

	if follow {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "streaming daemon logs")
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "showing daemon logs")
	}

	runJournalStream(cmd, buildJournalArgs(c.cfg.UserUnit, lines, follow))
}

// launchdDaemonController is the macOS analog of systemdDaemonController. It keeps
// baseDir so Logs can tail the daemon's own log file (see tailDaemonLogFile) — launchd,
// unlike systemd/journald, has no persistent unified log to delegate to.
type launchdDaemonController struct {
	baseDir string
	cfg     config.LaunchdConfig
}

// domain returns "system" for a LaunchDaemon, or "gui/<uid>" for a LaunchAgent —
// resolving the target user's uid via userutil.EffectiveUser() since os.Getuid() is 0
// under sudo while the LaunchAgent's gui session belongs to the invoking user.
func (c launchdDaemonController) domain() string {
	if !c.cfg.UserAgent {
		return "system"
	}
	uid := os.Getuid()
	if effectiveUser, err := userutil.EffectiveUser(); err == nil {
		if euid, _, credErr := userutil.UserCredentials(effectiveUser); credErr == nil {
			uid = int(euid)
		}
	}
	return launchdDomain(true, uid)
}

func (c launchdDaemonController) target() string {
	return c.domain() + "/" + launchdLabel(c.cfg.LaunchdPlistFileName)
}

func (c launchdDaemonController) Start(ctx context.Context, _ bool, _ bool, _ bool) error {
	if !c.cfg.UserAgent && os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	plistPath := filepath.Join(c.cfg.LaunchdTargetDir, c.cfg.LaunchdPlistFileName)
	launchctlPath, err := helpers.ResolveExecutable("launchctl")
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, launchctlPath, "bootstrap", c.domain(), plistPath).CombinedOutput() // #nosec G204 -- args are a fixed set built from config, not external input; launchctlPath resolved via LookPath
	if err != nil {
		return fmt.Errorf("starting launchd service: %s", out)
	}
	return nil
}

// Stop uses "launchctl bootout", which stops the job and unloads it — the plist stays
// on disk so it starts again at next boot/login, matching systemd stop's behavior of
// stopping now without disabling the unit. Exit code 3 ("No such process") means the
// job wasn't loaded, i.e. already stopped — treated the same as systemd stop on an
// already-stopped unit (killed=false, no error), verified empirically via launchctl.
func (c launchdDaemonController) Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	target := c.target()
	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	launchctlPath, err := helpers.ResolveExecutable("launchctl")
	if err != nil {
		return false, err
	}
	out, err := exec.CommandContext(ctx, launchctlPath, "bootout", target).CombinedOutput() // #nosec G204 -- args are a fixed set built from config, not external input; launchctlPath resolved via LookPath
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			helpers.Debugf(cmd, verbose, "launchctl bootout: job was not loaded")
			return false, nil
		}
		helpers.Debugf(cmd, verbose, "launchctl exited with error: %s", strings.TrimSpace(string(out)))
		return false, fmt.Errorf("stopping launchd service: %s", out)
	}
	helpers.Debugf(cmd, verbose, "launchctl bootout succeeded")
	return true, nil
}

// IsRunning always reports true: unlike systemd's "is-active" or standalone's
// PID/socket check, there's no cheap, reliable "is this launchd job loaded"
// primitive in this codebase (daemonIsDown() treats launchd the same way, as
// "not confirmed down"). Assuming running preserves the existing prompt-first
// behavior rather than risking a false "not running" skip.
func (c launchdDaemonController) IsRunning(_ context.Context) bool {
	return true
}

func (c launchdDaemonController) Remove() error {
	return os.Remove(filepath.Join(c.cfg.LaunchdTargetDir, c.cfg.LaunchdPlistFileName))
}

func (c launchdDaemonController) Info(cmd *cobra.Command) {
	printLaunchdDaemonDetails(cmd, c.cfg.UserAgent)
}

func (c launchdDaemonController) LogsHint() string {
	return eosDaemonLogsCmdName
}

func (c launchdDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	tailDaemonLogFile(cmd, c.baseDir, config.DaemonLogFileName, lines, follow)
}

// openrcDaemonController is the OpenRC analog of systemdDaemonController. It
// delegates the daemon lifecycle to rc-service so eos honors — rather than
// fights — the supervise-daemon that OpenRC's init script installs (issue #13):
// signaling the daemon PID directly only makes supervise-daemon respawn it
// within respawn_delay, so a direct-signal "stop" reports a false success while
// the daemon comes right back. Only "rc-service eos stop" actually stops it.
//
// Like launchd, OpenRC has no unified journal to delegate log reads to, so Logs
// tails the daemon's own rotated log file (see tailDaemonLogFile). The run field
// is injectable so tests can drive Start/Stop without a real rc-service.
type openrcDaemonController struct {
	run     runCmdFn
	cfg     config.OpenRCConfig
	baseDir string
}

// unit returns the OpenRC service name (the init script's file name).
func (c openrcDaemonController) unit() string {
	if c.cfg.InitFileName != "" {
		return c.cfg.InitFileName
	}
	return config.OpenRCTargetFileName
}

func (c openrcDaemonController) Start(ctx context.Context, _ bool, _ bool, _ bool) error {
	if os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	out, err := c.run(ctx, rcServiceCmdName, c.unit(), "start")
	if err != nil {
		return fmt.Errorf("starting OpenRC service: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (c openrcDaemonController) Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	if os.Getuid() != 0 {
		return false, errors.New("requires root — run with sudo")
	}
	unit := c.unit()
	helpers.Debugf(cmd, verbose, "running: rc-service %s stop", unit)
	out, err := c.run(ctx, rcServiceCmdName, unit, "stop")
	if err != nil {
		helpers.Debugf(cmd, verbose, "rc-service exited with error: %s", strings.TrimSpace(string(out)))
		return false, fmt.Errorf("stopping OpenRC service: %s", strings.TrimSpace(string(out)))
	}
	helpers.Debugf(cmd, verbose, "rc-service stop succeeded")
	return true, nil
}

// IsRunning uses rc-service's own status check — OpenRC services are already
// one-per-base-dir (the init script is generated per base dir), so unlike
// systemd's host-global `systemctl is-active` this carries no cross-base-dir
// ambiguity to guard against.
func (c openrcDaemonController) IsRunning(ctx context.Context) bool {
	_, err := c.run(ctx, rcServiceCmdName, c.unit(), "status")
	return err == nil
}

func (c openrcDaemonController) Remove() error {
	return os.Remove(filepath.Join(c.cfg.InitDir, c.unit()))
}

func (c openrcDaemonController) Info(cmd *cobra.Command) {
	printOpenRCDaemonDetails(cmd)
}

func (c openrcDaemonController) LogsHint() string {
	return eosDaemonLogsCmdName
}

func (c openrcDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	tailDaemonLogFile(cmd, c.baseDir, config.DaemonLogFileName, lines, follow)
}

func newDaemonController(cfg config.DaemonConfig, baseDir string, health *config.HealthConfig, shutdown config.ShutdownConfig, telemetry config.TelemetryConfig, underSystemd bool, identity userutil.Identity) (DaemonController, error) {
	if cfg.Standalone != nil {
		return &standaloneDaemonController{
			cfg:          *cfg.Standalone,
			baseDir:      baseDir,
			health:       *health,
			shutdown:     shutdown,
			telemetry:    telemetry,
			underSystemd: underSystemd,
			identity:     identity,
		}, nil
	}
	if cfg.Systemd != nil {
		return systemdDaemonController{cfg: *cfg.Systemd, checkDir: isAccessibleDir}, nil
	}
	if cfg.Launchd != nil {
		return launchdDaemonController{cfg: *cfg.Launchd, baseDir: baseDir}, nil
	}
	if cfg.OpenRC != nil {
		return openrcDaemonController{cfg: *cfg.OpenRC, baseDir: baseDir, run: execRunCmd}, nil
	}
	return nil, errors.New("invalid daemon config: standalone, systemd, launchd, and openrc are all nil")
}

// buildDaemonSubcmds attaches all daemon subcommands to daemonCmd.
// getCtrl is called at Run time; in production it returns the controller set by
// PersistentPreRun, and in tests it returns a mock.
func buildDaemonSubcmds(daemonCmd *cobra.Command, getCtrl func() DaemonController) {
	startCmd := daemonCmdBuildStartCmd(getCtrl)
	daemonCmdMarkLogFlagHidden(daemonCmd, startCmd)

	daemonCmd.AddCommand(daemonCmdBuildInfoCmd(getCtrl))
	daemonCmd.AddCommand(daemonCmdBuildLogsCmd(getCtrl))
	daemonCmd.AddCommand(daemonCmdBuildRemoveCmd(getCtrl))
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(daemonCmdBuildStopCmd(getCtrl))
}

// daemonCmdMarkLogFlagHidden hides startCmd's internal --log-to-file-and-console
// flag (used only by the detached child spawned via buildForkCommand) from
// `eos daemon start --help`.
func daemonCmdMarkLogFlagHidden(daemonCmd *cobra.Command, startCmd *cobra.Command) {
	if err := startCmd.Flags().MarkHidden(flagLogToFileAndConsole); err != nil {
		daemonCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("marking daemon flag as hidden: %v", err))
	}
}

// daemonCmdBuildStartCmd constructs the "eos daemon start" subcommand.
func daemonCmdBuildStartCmd(getCtrl func() DaemonController) *cobra.Command {
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon process",
		Long: `Launch the deployment daemon.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).
If an OpenRC init script is installed, delegates to "rc-service eos start" (requires root).

Otherwise, starts the daemon detached in the background by default; control returns once the PID file is written (timeout: 5s). --detach (-d) is accepted for backward compatibility but is now a no-op. Pass --foreground (-f) to run in the foreground and stream output to the console instead — Ctrl-C will then stop the daemon.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonCmdRunStart(cmd, getCtrl)
		},
	}
	startCmd.Flags().BoolP("foreground", "f", false, "run daemon in foreground and stream output (Ctrl-C stops it)")
	startCmd.Flags().BoolP("detach", "d", false, "run daemon in background (default; kept for backward compatibility)")
	startCmd.Flags().Bool(flagLogToFileAndConsole, false, "")
	return startCmd
}

// daemonCmdRunStart implements startCmd's RunE: resolves the foreground/detach
// flags, starts the daemon via ctrl, and reports the outcome.
func daemonCmdRunStart(cmd *cobra.Command, getCtrl func() DaemonController) error {
	ctrl := getCtrl()
	detach, logToFileAndConsole, verbose, err := daemonCmdParseStartFlags(cmd)
	if err != nil {
		return err
	}

	daemonCmdPrintStarting(cmd, detach)

	if err := ctrl.Start(cmd.Context(), detach, logToFileAndConsole, verbose); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		return helpers.ErrCommandFailed
	}

	if detach {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "daemon started in background")
		cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos daemon info"), ui.TextMuted.Render("to check daemon service status"))
	}
	return nil
}

// daemonCmdParseStartFlags reads and validates startCmd's flags, resolving
// --foreground/--detach into a single detach decision.
func daemonCmdParseStartFlags(cmd *cobra.Command) (detach bool, logToFileAndConsole bool, verbose bool, err error) {
	foreground, err := cmd.Flags().GetBool("foreground")
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
		return false, false, false, helpers.ErrCommandFailed
	}
	detachFlag, err := cmd.Flags().GetBool("detach")
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
		return false, false, false, helpers.ErrCommandFailed
	}
	if foreground && detachFlag {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "cannot use --foreground and --detach together")
		return false, false, false, helpers.ErrCommandFailed
	}
	logToFileAndConsole, _ = cmd.Flags().GetBool(flagLogToFileAndConsole)
	verbose, _ = cmd.Flags().GetBool("verbose")
	return !foreground, logToFileAndConsole, verbose, nil
}

// daemonCmdPrintStarting prints the "starting daemon..." message, worded for
// whether the daemon is starting detached or in the foreground.
func daemonCmdPrintStarting(cmd *cobra.Command, detach bool) {
	if detach {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "starting daemon in background...")
		return
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "starting daemon in foreground — press Ctrl-C to stop this daemon...")
}

// daemonCmdBuildStopCmd constructs the "eos daemon stop" subcommand.
func daemonCmdBuildStopCmd(getCtrl func() DaemonController) *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "Stop the running daemon",
		Long:          "Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root); if managed by OpenRC, delegates to rc-service stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonCmdRunStop(cmd, getCtrl)
		},
	}
}

// daemonCmdRunStop implements stopCmd's RunE: stops the daemon via ctrl and
// reports whether it was actually running.
func daemonCmdRunStop(cmd *cobra.Command, getCtrl func() DaemonController) error {
	ctrl := getCtrl()
	verbose, _ := cmd.Flags().GetBool("verbose")
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "stopping daemon...")
	killed, err := ctrl.Stop(cmd.Context(), cmd, verbose)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", err))
		return helpers.ErrCommandFailed
	}
	if !killed {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
		return nil
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "daemon stopped")
	return nil
}

// daemonCmdBuildRemoveCmd constructs the "eos daemon remove" subcommand.
func daemonCmdBuildRemoveCmd(getCtrl func() DaemonController) *cobra.Command {
	return &cobra.Command{
		Use:           "remove",
		Short:         "Remove a stopped daemon",
		Long:          "Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonCmdRunRemove(cmd, getCtrl)
		},
	}
}

// daemonCmdRunRemove implements removeCmd's RunE: removes the daemon's files
// via ctrl and reports the outcome.
func daemonCmdRunRemove(cmd *cobra.Command, getCtrl func() DaemonController) error {
	ctrl := getCtrl()
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "removing daemon...")
	if err := ctrl.Remove(); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("removing daemon: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "daemon removed")
	cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos system unstartup"), ui.TextMuted.Render("to undo systemd startup"))
	return nil
}

// daemonCmdBuildInfoCmd constructs the "eos daemon info" subcommand.
func daemonCmdBuildInfoCmd(getCtrl func() DaemonController) *cobra.Command {
	var allUsers bool
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show daemon status and configuration",
		Long:  "Display daemon status and configuration. For systemd-managed daemons, shows configuration only (use 'systemctl status eos.service' for runtime state). For standalone daemons, shows whether the process is running, its PID, socket path, log directory, log file name, max file count, and file size limit. Reports clearly if the daemon is stopped or not found.\n\nPass --all (root only) to enumerate every user's standalone daemon on the host instead of just the invoking user's, flagging any still running against a since-replaced binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonCmdRunInfo(cmd, getCtrl, allUsers)
		},
	}
	infoCmd.Flags().BoolVar(&allUsers, "all", false, "list every user's standalone daemon on this host (root only)")
	return infoCmd
}

// daemonCmdRunInfo implements infoCmd's RunE: either enumerates every user's
// daemon (--all) or reports the invoking user's daemon status via ctrl.
func daemonCmdRunInfo(cmd *cobra.Command, getCtrl func() DaemonController, allUsers bool) error {
	if allUsers {
		return printAllDaemons(cmd)
	}
	ctrl := getCtrl()
	ctrl.Info(cmd)
	return nil
}

// daemonCmdBuildLogsCmd constructs the "eos daemon logs" subcommand.
func daemonCmdBuildLogsCmd(getCtrl func() DaemonController) *cobra.Command {
	var lines int
	var follow bool
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "View daemon log output",
		Long:  "Display or stream the daemon's log file. Defaults to the last 300 lines. Use --follow to tail in real time, --lines to control history depth. Accepts values between 0 and 10,000. Exit with Ctrl+C.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonCmdRunLogs(cmd, getCtrl, lines, follow)
		},
	}
	logsCmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display")
	logsCmd.Flags().BoolVar(&follow, "follow", false, "follow log output")
	return logsCmd
}

// daemonCmdRunLogs implements logsCmd's RunE: streams or displays the
// daemon's log output via ctrl.
func daemonCmdRunLogs(cmd *cobra.Command, getCtrl func() DaemonController, lines int, follow bool) error {
	ctrl := getCtrl()
	ctrl.Logs(cmd, lines, follow)
	return nil
}

// resolveDaemonControllerPreRun loads the system config via getConfig and
// builds the DaemonController for it, printing an error and exiting on
// either failure. Shared by newSystemCmd and newDaemonCmd, whose
// PersistentPreRun bodies were otherwise byte-identical.
func resolveDaemonControllerPreRun(cmd *cobra.Command, getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) DaemonController {
	baseDir, systemConfig, identity, err := getConfig()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
		os.Exit(1)
		return nil
	}
	if systemConfig == nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "getting config: got a nil system config")
		os.Exit(1)
		return nil
	}
	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.Telemetry, systemConfig.UnderSystemd, identity)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving daemon mode: %v", err))
		os.Exit(1)
		return nil
	}
	return ctrl
}

func newDaemonCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	var ctrl DaemonController

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
		Long:  "Commands for controlling and monitoring the long-running deployment daemon process. Use start/stop to control the lifecycle, remove to clean up daemon files, info to inspect its current status, and logs to stream its output.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			ctrl = resolveDaemonControllerPreRun(cmd, getConfig)
		},
	}

	buildDaemonSubcmds(daemonCmd, func() DaemonController { return ctrl })

	return daemonCmd
}

// envWithout returns env with any entries for key removed.
func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// Stay in sync with "startDaemonProcess"
// buildForkCommand constructs the detached `eos daemon start` command. When the
// parent runs as root it drops the child to identity's uid/gid and strips root's
// HOME so the child resolves paths under its own home, not /root.
//
// The child's stderr is wired to a real file (manager.OpenForkStderrLog), not an
// in-process io.Writer: os/exec creates a real OS pipe for any Stderr that isn't
// an *os.File, and the pipe's read end lives in this process. Once this process
// exits — moments after reporting success back to the shell — that pipe is
// orphaned, and the detached child gets SIGPIPE'd on its next stderr write (see
// issue #156). A real file has no reader to lose: fork/exec gives the child its
// own independent file descriptor regardless of what this process does with it.
func buildForkCommand(ctx context.Context, exePath string, verbose bool, identity userutil.Identity, pidFile string) (*exec.Cmd, *os.File, error) {
	// --foreground is required here: the child inherits no flags, and "daemon start"
	// now defaults to detach=true, so without it the child would fork again and again.
	args := []string{"daemon", "start", "--foreground", "--log-to-file-and-console"}
	if verbose {
		args = append(args, "--verbose")
	}
	cmd := exec.CommandContext(ctx, exePath, args...) // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil

	stderrFile, err := manager.OpenForkStderrLog(pidFile)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = stderrFile

	if os.Getuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: identity.UID(), Gid: identity.GID()}

		// The child drops to identity's uid/gid, but without this it would inherit
		// root's HOME from the parent's environment (sudo sets HOME=/root).
		// os.UserHomeDir() and friends would then resolve paths under /root,
		// which the dropped-privilege child can't even stat (root's home is 0700).
		cmd.Env = append(envWithout(os.Environ(), "HOME"), "HOME="+identity.HomeDir())
	}
	return cmd, stderrFile, nil
}

// waitForForkPIDFile blocks until the forked daemon is confirmed alive via its
// PID file, or the 5s deadline elapses. It re-checks process liveness (not
// just file existence): a PID file can exist for an instant before the
// process that wrote it dies.
func waitForForkPIDFile(pidFile string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := process.StatusStandaloneDaemon(&config.StandaloneDaemonConfig{PIDFile: pidFile})
		if err == nil && status.Running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for PID file: %s", pidFile)
}

// waitForForkSocket blocks until the forked daemon's Unix socket accepts a
// connection, or the 5s deadline elapses. A PID file can exist — and its
// process still be alive — for a brief window before an unrelated startup
// failure (e.g. a socket bind error) kills it moments later; confirming the
// socket answers is what actually proves the daemon reached a running state,
// not just that it forked (issue #156).
func waitForForkSocket(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if socketResponds(ctx, socketPath) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon socket: %s", socketPath)
}

// forkDaemon starts a new detached daemon process: refuse if one is already
// running, spawn the child, then confirm it is durably alive before
// reporting success (issue #156). Each step is a small, independently tested
// helper so this orchestrator stays a plain three-step sequence.
func forkDaemon(ctx context.Context, cfg *config.StandaloneDaemonConfig, verbose bool, identity userutil.Identity) error {
	if err := ensureDaemonNotRunning(cfg); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	if err := spawnForkedDaemon(ctx, exePath, verbose, identity, cfg.PIDFile); err != nil {
		return err
	}

	return confirmForkAlive(ctx, cfg)
}

// restartDaemonStandaloneIfConfirmed prompts to restart the daemon standalone
// after a startup/unstartup change, forking it and reporting the outcome.
// Shared by system.go's unstartupCmd/unstartupCmdLaunchd and
// system_openrc.go's openrcUnstartupCmd, which otherwise repeat this
// prompt-fork-report sequence verbatim.
func restartDaemonStandaloneIfConfirmed(ctx context.Context, cmd *cobra.Command, flagYes bool, identity userutil.Identity) error {
	if !confirmOrDecline(cmd, flagYes, "restart daemon standalone? (y/n):", "") {
		return nil
	}
	if err := forkDaemon(ctx, &config.StandaloneDaemonConfig{PIDFile: config.DaemonPIDFile, SocketPath: config.DaemonSocketPath}, false, identity); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(eosDaemonLogsCmdName) + ui.TextMuted.Render(msgCheckDaemonLogs) + "\n")
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), msgDaemonStartedBg)
	return nil
}

// ensureDaemonNotRunning errors if a live daemon already holds cfg's PID
// file. A fork while one is running spawns a child that fails to bind and
// exits quietly, which previously looked identical to success (issue #156).
func ensureDaemonNotRunning(cfg *config.StandaloneDaemonConfig) error {
	status, err := process.StatusStandaloneDaemon(cfg)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if status.Running {
		return fmt.Errorf("daemon already running (pid %d)", *status.Pid)
	}
	return nil
}

// spawnForkedDaemon starts the detached daemon child. Once cmd.Start()
// returns, this process's copy of the child's stderr fd is no longer needed:
// fork/exec already gave the child its own independent one (see
// buildForkCommand).
func spawnForkedDaemon(ctx context.Context, exePath string, verbose bool, identity userutil.Identity, pidFile string) error {
	cmd, stderrFile, err := buildForkCommand(ctx, exePath, verbose, identity, pidFile)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stderrFile.Close()
		return fmt.Errorf("failed to start daemon process: %w", err)
	}
	_ = stderrFile.Close()
	return nil
}

// confirmForkAlive waits for the forked child to become durably alive: PID
// file liveness first (required for Type=forking: systemd reads the PID file
// immediately after this process exits), then the daemon's socket actually
// answering — a PID file can exist, and its process still be alive, for a
// brief window before an unrelated startup failure (e.g. a socket bind
// error) kills it moments later (issue #156).
func confirmForkAlive(ctx context.Context, cfg *config.StandaloneDaemonConfig) error {
	if err := waitForForkPIDFile(cfg.PIDFile); err != nil {
		return forkStartupErr(err, cfg.PIDFile)
	}
	if err := waitForForkSocket(ctx, cfg.SocketPath); err != nil {
		return forkStartupErr(err, cfg.PIDFile)
	}
	return nil
}

// forkStartupErr folds any captured child stderr into a fork startup failure,
// whichever of the two readiness waits (PID file, socket) timed out first.
func forkStartupErr(err error, pidFile string) error {
	if output := manager.ReadForkStderr(pidFile); output != "" {
		return fmt.Errorf("%w\nchild stderr: %s", err, output)
	}
	return err
}

func printAllDaemons(cmd *cobra.Command) error {
	if os.Getuid() != 0 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "--all requires root — run with sudo")
		return helpers.ErrCommandFailed
	}

	daemons, err := process.DiscoverDaemons()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("discovering daemons: %v", err))
		return helpers.ErrCommandFailed
	}

	renderDaemonSummaries(cmd, daemons)
	return nil
}

// renderDaemonSummaries prints one line per discovered daemon plus a trailing restart hint
// if any are stale. Split out from printAllDaemons so it can be tested without root.
func renderDaemonSummaries(cmd *cobra.Command, daemons []process.DaemonSummary) {
	if len(daemons) == 0 {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render("no standalone daemons found on this host"))
		return
	}

	staleCount := 0
	for _, d := range daemons {
		if d.Err != nil {
			cmd.Printf(fmtLabelMsgLn, ui.LabelError.Render("✗"), d.Username)
			cmd.Printf(fmtIndentLabelAny, ui.TextMuted.Render("error:"), d.Err)
			continue
		}
		if !d.Status.Running {
			cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("○"), d.Username, ui.TextMuted.Render("not running"))
			continue
		}
		if d.StaleBinary {
			staleCount++
			cmd.Printf("%s %s %s\n", ui.LabelWarning.Render("⚠"), d.Username, ui.TextMuted.Render(fmt.Sprintf("running (pid %d) — on a since-replaced binary, restart needed", *d.Status.Pid)))
			continue
		}
		cmd.Printf("%s %s %s\n", ui.LabelSuccess.Render("✓"), d.Username, ui.TextMuted.Render(fmt.Sprintf("running (pid %d)", *d.Status.Pid)))
	}
	cmd.Println()

	if staleCount > 0 {
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), fmt.Sprintf("%d daemon(s) still running the pre-update binary", staleCount))
		cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render("sudo -u <user> eos daemon stop && sudo -u <user> eos daemon start") + ui.TextMuted.Render(" → restart each") + "\n\n")
	}
}

func printSystemdDaemonDetails(cmd *cobra.Command, cfg config.SystemdConfig) {
	statusCmd, logsCmd := daemonCmdSystemdCommandHints(cfg.UserUnit)
	if cfg.UserUnit {
		daemonCmdWarnSystemdUserBus(cmd)
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon is systemd managed"))
	daemonCmdPrintSystemdRunState(cmd, cfg)
	daemonCmdPrintSystemdVersion(cmd, cfg)
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(statusCmd) + ui.TextMuted.Render(" → check systemd service status") + "\n")
	cmd.Printf(fmtHeading, ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(logsCmd) + ui.TextMuted.Render(" → check journalctl service logs") + "\n")
	cmd.Println()
}

// daemonCmdSystemdCommandHints returns the systemctl/journalctl invocations to
// suggest to the user, scoped to the user bus when the unit is a user unit.
func daemonCmdSystemdCommandHints(userUnit bool) (statusCmd string, logsCmd string) {
	if userUnit {
		return "systemctl --user status eos.service", "journalctl --user -u eos.service"
	}
	return "systemctl status eos.service", "journalctl -u eos.service"
}

// daemonCmdWarnSystemdUserBus prints a warning when the invoking session has
// no reachable systemd user bus, which would otherwise make the suggested
// systemctl/journalctl --user commands fail with "Failed to connect to bus".
func daemonCmdWarnSystemdUserBus(cmd *cobra.Command) {
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		return
	}
	effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
	if credErr != nil {
		return
	}
	uid := int(effectiveUID)
	if systemdUserBusReachable(uid) {
		return
	}
	cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), "no active systemd user bus — the commands below will fail with \"Failed to connect to bus\"")
	cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), fmt.Sprintf("run %s, then start a fresh login session (or export XDG_RUNTIME_DIR=/run/user/%d in this shell)", ui.TextCommand.Render("sudo loginctl enable-linger "+effectiveUser.Username), uid))
}

// daemonCmdPrintSystemdRunState prints whether the systemd-managed daemon is
// currently running (and its PID when resolvable), based on the same
// base-dir-scoped socket probe daemonIsDown() uses.
func daemonCmdPrintSystemdRunState(cmd *cobra.Command, cfg config.SystemdConfig) {
	if !socketResponds(cmd.Context(), cfg.SocketPath) {
		cmd.Printf(fmtIndentLabelMsgLn, ui.LabelInfo.Render("○"), ui.TextMuted.Render("not running"))
		return
	}
	if pid, err := systemdMainPID(cmd.Context(), cfg.UserUnit); err == nil {
		cmd.Printf(fmtIndentLabelMsgLn, ui.LabelSuccess.Render("✓"), fmt.Sprintf("running (pid %d)", pid))
		return
	}
	cmd.Printf(fmtIndentLabelMsgLn, ui.LabelSuccess.Render("✓"), "running")
}

// daemonCmdPrintSystemdVersion prints the version embedded in the binary
// actually backing the running systemd-managed daemon process, when it can
// be resolved.
func daemonCmdPrintSystemdVersion(cmd *cobra.Command, cfg config.SystemdConfig) {
	version, err := systemdDaemonRunningVersion(cmd.Context(), cfg.UserUnit)
	if err != nil {
		return
	}
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("running version:"), version)
}

// systemdUserBusReachable reports whether $XDG_RUNTIME_DIR, as exported in this process's own
// environment, points at a dir accessible to uid — the same signal `systemctl --user` itself
// relies on to find the session bus socket. It deliberately does not fall back to checking
// whether userRuntimeDir(uid) exists on disk: a lingering user manager (via `loginctl
// enable-linger`) can leave /run/user/<uid> present and accessible while this process's
// environment simply never had the var exported, which is exactly the case the caller's warning
// exists to catch — falling back to the derived path masks it.
func systemdUserBusReachable(uid int) bool {
	return isAccessibleDir(os.Getenv("XDG_RUNTIME_DIR"), uid)
}

// systemdMainPID resolves the PID systemd currently attributes to the "eos"
// unit's MainPID property. Safe to treat as this daemon's PID only once the
// caller has independently confirmed liveness via the base-dir-scoped socket
// probe (socketResponds): MainPID itself is queried against the one literal
// "eos" unit for this user/system scope, with no base-dir awareness of its
// own — the same host/user-global blind spot documented on
// config.SystemdConfig.SocketPath (issue #12).
func systemdMainPID(ctx context.Context, userUnit bool) (int, error) {
	systemctlPath, err := helpers.ResolveExecutable("systemctl")
	if err != nil {
		return 0, err
	}
	pidOut, err := exec.CommandContext(ctx, systemctlPath, systemctlArgs(userUnit, "show", "-p", "MainPID", "--value", "eos")...).Output() // #nosec G204 -- args are a fixed set built from a bool, not external input; systemctlPath resolved via LookPath
	if err != nil {
		return 0, fmt.Errorf("querying systemd for daemon pid: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidOut)))
	if err != nil || pid <= 0 {
		return 0, errors.New("daemon is not running")
	}
	return pid, nil
}

// systemdDaemonRunningVersion resolves the version string embedded in the binary
// actually backing the running systemd-managed daemon process, by following
// /proc/<pid>/exe rather than trusting the currently installed binary — the two
// differ exactly when the unit hasn't been restarted since an update replaced the
// binary on disk (the same drift StaleBinary detects for standalone daemons).
func systemdDaemonRunningVersion(ctx context.Context, userUnit bool) (string, error) {
	pid, err := systemdMainPID(ctx, userUnit)
	if err != nil {
		return "", err
	}
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", fmt.Errorf("resolving daemon binary: %w", err)
	}
	out, err := exec.CommandContext(ctx, exePath, "--version").Output() // #nosec G204 -- exePath resolved from the daemon's own /proc/<pid>/exe, not external input
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", exePath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func printLaunchdDaemonDetails(cmd *cobra.Command, userAgent bool) {
	statusCmd := "sudo launchctl print system/" + config.LaunchdLabel
	scope := "launch daemon"
	if userAgent {
		scope = "launch agent"
		statusCmd = "launchctl print gui/$(id -u)/" + config.LaunchdLabel
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), ui.TextMuted.Render(fmt.Sprintf("daemon is launchd managed (%s)", scope)))
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(statusCmd) + ui.TextMuted.Render(" → check launchd service status") + "\n")
	cmd.Printf(fmtHeading, ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(eosDaemonLogsCmdName) + ui.TextMuted.Render(" → tail daemon log file") + "\n")
	cmd.Println()
}

func printOpenRCDaemonDetails(cmd *cobra.Command) {
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon is OpenRC managed"))
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render("rc-service eos status") + ui.TextMuted.Render(" → check OpenRC service status") + "\n")
	cmd.Printf(fmtHeading, ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(eosDaemonLogsCmdName) + ui.TextMuted.Render(" → tail daemon log file") + "\n")
	cmd.Println()
}

func printStandaloneDaemonDetails(cmd *cobra.Command, pid int, cfg *config.StandaloneDaemonConfig) {
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("PID:"), pid)
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("pid file:"), cfg.PIDFile)
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket path:"), cfg.SocketPath)
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("socket timeout:"), cfg.SocketTimeout)
	cmd.Printf(fmtHeading, ui.TextBold.Render("Logging"))
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("log dir:"), cfg.Log.LogDir)
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("log file:"), cfg.Log.LogFileName)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("log max files:"), cfg.Log.LogMaxFiles)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("log file size limit:"), cfg.Log.LogFileSizeLimit)
}
