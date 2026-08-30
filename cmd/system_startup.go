package cmd

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// sysRunStartup backs the "system startup" subcommand's RunE: it resolves
// shared config/flags, then dispatches to the darwin or systemd/OpenRC path.
func sysRunStartup(cmd *cobra.Command, systemCmd *cobra.Command) error {
	installDir, systemConfig, _, verbose, flagYes, err := loadSystemConfigAndFlags(cmd, systemCmd)
	if err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		return sysStartupDarwin(cmd, systemCmd, installDir, systemConfig.Daemon.Standalone, verbose, flagYes)
	}
	return sysStartupLinux(cmd, systemCmd, installDir, systemConfig.Daemon.Standalone, verbose, flagYes, detectActiveSystemRuntime)
}

// sysStartupDarwin handles "system startup" on macOS: resolve the launchd
// target directory (system LaunchDaemons, or the invoking user's
// LaunchAgents when unprivileged) and install the plist.
func sysStartupDarwin(cmd *cobra.Command, systemCmd *cobra.Command, installDir string, standalone *config.StandaloneDaemonConfig, verbose, flagYes bool) error {
	userAgent := os.Getuid() != 0
	launchdDir := config.LaunchdTargetDir
	if userAgent {
		var err error
		launchdDir, err = config.UserLaunchAgentsDir()
		if err != nil {
			systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving user launch agents dir: %v", err))
			return helpers.ErrCommandFailed
		}
	}
	return startupCmdLaunchd(cmd.Context(), cmd, launchdStartupParams{
		InstallDir:    installDir,
		DaemonConfig:  standalone,
		LaunchdDir:    launchdDir,
		PlistFileName: config.LaunchdPlistFileName,
		UserAgent:     userAgent,
		Verbose:       verbose,
		FlagYes:       flagYes,
	}, execRunCmd)
}

// sysStartupLinux handles "system startup" on non-macOS platforms: detect
// the active init system and install an OpenRC init script or a systemd
// unit (system-wide or the invoking user's, when unprivileged). detectRuntime
// is injected (like startupCmd/openrcStartupCmd already do) so tests can
// drive both dispatch branches deterministically instead of depending on the
// real host's init system.
func sysStartupLinux(cmd *cobra.Command, systemCmd *cobra.Command, installDir string, standalone *config.StandaloneDaemonConfig, verbose, flagYes bool, detectRuntime func() (string, error)) error {
	runtimeName, err := detectRuntime()
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}

	if runtimeName == "openrc" {
		return openrcStartupCmd(cmd.Context(), cmd, openrcStartupParams{
			InstallDir:   installDir,
			DaemonConfig: standalone,
			InitDir:      config.OpenRCInitDir,
			InitFile:     config.OpenRCTargetFileName,
			Verbose:      verbose,
			FlagYes:      flagYes,
		}, detectRuntime, execRunCmd)
	}

	userUnit := os.Getuid() != 0
	systemdDir := config.SystemdTargetDir
	if userUnit {
		var err error
		systemdDir, err = config.UserSystemdDir()
		if err != nil {
			systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving user systemd dir: %v", err))
			return helpers.ErrCommandFailed
		}
	}
	return startupCmd(cmd.Context(), cmd, systemdStartupParams{
		InstallDir:   installDir,
		DaemonConfig: standalone,
		SystemdDir:   systemdDir,
		SystemdFile:  config.SystemdTargetFileName,
		UserUnit:     userUnit,
		Verbose:      verbose,
		FlagYes:      flagYes,
	}, detectRuntime, execRunCmd)
}

// systemdStartupParams bundles the plain-value parameters startupCmd needs to
// render, write, and enable a systemd unit file.
type systemdStartupParams struct {
	InstallDir   string
	DaemonConfig *config.StandaloneDaemonConfig
	SystemdDir   string
	SystemdFile  string
	UserUnit     bool
	Verbose      bool
	FlagYes      bool
}

func startupCmd(ctx context.Context, cmd *cobra.Command, p systemdStartupParams, detectRuntime func() (string, error), run runCmdFn) error {
	if err := ensureRuntime(cmd, p.Verbose, detectRuntime, "systemd"); err != nil {
		return err
	}

	fullTargetName := filepath.Join(p.SystemdDir, p.SystemdFile)
	helpers.Debugf(cmd, p.Verbose, "target unit file: %s", fullTargetName)

	if err := ensureSystemdUnitDir(cmd, p.Verbose, p.UserUnit, p.SystemdDir, fullTargetName); err != nil {
		return err
	}

	effectiveUser, unitFile, err := resolveSystemdUnitFile(cmd, p.Verbose, p.InstallDir, p.UserUnit)
	if err != nil {
		return err
	}

	unitKind := unitScope(p.UserUnit) + " file"

	if !confirmOrDecline(cmd, p.FlagYes, fmt.Sprintf("create %s? (y/n):", unitKind), unitKind+" creation canceled") {
		return nil
	}

	if err = os.WriteFile(fullTargetName, []byte(unitFile), 0644); err != nil { // #nosec G306 -- unit files should be readable by other users/tools
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("writing unit file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(unitKind+" created, at:"), fullTargetName)

	if p.UserUnit {
		if busErr := prepareUserBus(ctx, cmd, p.Verbose, effectiveUser, run); busErr != nil {
			return busErr
		}
	}

	unit := unitName(p.SystemdFile)
	if enableErr := enableSystemdUnit(ctx, cmd, p.Verbose, p.UserUnit, unit, run); enableErr != nil {
		return enableErr
	}

	if p.UserUnit {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "user unit enabled, eos will start on login")
		offerEnableLinger(ctx, cmd, p.FlagYes, effectiveUser.Username, run)
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "system unit enabled, eos will start on boot")
	}

	if !confirmOrDecline(cmd, p.FlagYes, "restart daemon now? (y/n):", "daemon will be managed by systemd on next start") {
		return nil
	}

	if stopErr := stopStandaloneForRestart(cmd, p.DaemonConfig); stopErr != nil {
		return stopErr
	}

	helpers.Debugf(cmd, p.Verbose, "running: systemctl %s", strings.Join(systemctlArgs(p.UserUnit, "start", unit), " "))
	out, err := run(ctx, "systemctl", systemctlArgs(p.UserUnit, "start", unit)...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting systemd daemon: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, p.Verbose, "start output: %s", strings.TrimSpace(string(out)))
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

// resolveSystemdUnitFile resolves the invoking user and renders the systemd
// unit file content for them, printing and wrapping errors from either step.
func resolveSystemdUnitFile(cmd *cobra.Command, verbose bool, installDir string, userUnit bool) (*user.User, string, error) {
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return nil, "", helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "effective user: %s", effectiveUser.Username)

	unitFile, err := renderUnitFile(installDir, effectiveUser.Username, userUnit)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("rendering unit file: %v", err))
		return nil, "", helpers.ErrCommandFailed
	}
	return effectiveUser, unitFile, nil
}

type unitData struct {
	ExecStart string `json:"exec_start"` // absolute path to eos binary
	PIDFile   string `json:"pid_file"`   // absolute path to eos.pid
	User      string `json:"user"`
}

func renderUnitFile(installDir string, user string, userUnit bool) (string, error) {
	// StartLimitIntervalSec/StartLimitBurst bound the crash-loop: without them,
	// a persistent startup failure (e.g. a state database whose schema version
	// is ahead of this binary after a rollback) restarts forever because the
	// systemd default burst of 5 within a 10s window never trips at one restart
	// every RestartSec=5s. Widening the window to 60s lets 5 restarts land
	// inside it, so systemd gives up and enters "failed" instead of looping
	// indefinitely. This mirrors OpenRC's supervise-daemon --respawn-max 5.
	const systemUnitTemplate = `[Unit]
Description=eos deployment daemon
After=network.target
StartLimitIntervalSec=60s
StartLimitBurst=5

[Service]
Type=simple
ExecStart={{.ExecStart}} daemon start --foreground
Restart=always
RestartSec=5s
User={{.User}}

[Install]
WantedBy=multi-user.target`

	const userUnitTemplate = `[Unit]
Description=eos deployment daemon
After=network.target
StartLimitIntervalSec=60s
StartLimitBurst=5

[Service]
Type=simple
ExecStart={{.ExecStart}} daemon start --foreground
Restart=always
RestartSec=5s

[Install]
WantedBy=default.target`

	tmplStr := systemUnitTemplate
	if userUnit {
		tmplStr = userUnitTemplate
	}

	tmpl, err := template.New("unit").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	data := unitData{
		ExecStart: filepath.Join(installDir, "eos"),
		User:      user,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	return buf.String(), nil
}

// launchdStartupParams bundles the plain-value parameters startupCmdLaunchd
// needs to render, write, and enable a launchd plist file.
type launchdStartupParams struct {
	InstallDir    string
	DaemonConfig  *config.StandaloneDaemonConfig
	LaunchdDir    string
	PlistFileName string
	UserAgent     bool
	Verbose       bool
	FlagYes       bool
}

func startupCmdLaunchd(ctx context.Context, cmd *cobra.Command, p launchdStartupParams, run runCmdFn) error {
	fullTargetName := filepath.Join(p.LaunchdDir, p.PlistFileName)
	helpers.Debugf(cmd, p.Verbose, "target plist file: %s", fullTargetName)

	if err := ensureLaunchdDir(cmd, p.Verbose, p.UserAgent, p.LaunchdDir); err != nil {
		return err
	}

	label := launchdLabel(p.PlistFileName)
	effectiveUser, plistFile, err := resolveLaunchdPlistFile(cmd, p.Verbose, p.InstallDir, label, p.UserAgent)
	if err != nil {
		return err
	}

	plistKind := launchdScope(p.UserAgent) + " file"

	if !confirmOrDecline(cmd, p.FlagYes, fmt.Sprintf("create %s? (y/n):", plistKind), plistKind+" creation canceled") {
		return nil
	}

	if err = os.WriteFile(fullTargetName, []byte(plistFile), 0644); err != nil { // #nosec G306 -- plist files should be readable by other users/tools
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("writing plist file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(plistKind+" created, at:"), fullTargetName)

	uid, err := resolveLaunchdUID(cmd, p.UserAgent, effectiveUser)
	if err != nil {
		return err
	}
	domain := launchdDomain(p.UserAgent, uid)
	target := domain + "/" + label

	if bootErr := bootstrapLaunchdJob(ctx, cmd, p.Verbose, domain, target, fullTargetName, run); bootErr != nil {
		return bootErr
	}

	if p.UserAgent {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "launch agent enabled, eos will start on login")
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "launch daemon enabled, eos will start on boot")
	}

	if !confirmOrDecline(cmd, p.FlagYes, "restart daemon now? (y/n):", "daemon will be managed by launchd on next start") {
		return nil
	}

	if stopErr := stopStandaloneForRestart(cmd, p.DaemonConfig); stopErr != nil {
		return stopErr
	}

	helpers.Debugf(cmd, p.Verbose, "running: launchctl kickstart -k %s", target)
	out, err := run(ctx, "launchctl", "kickstart", "-k", target)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting launchd daemon: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, p.Verbose, "kickstart output: %s", strings.TrimSpace(string(out)))
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

// bootstrapLaunchdJob bootstraps and enables the plist job. bootout is
// attempted first (best-effort) so re-running is idempotent.
func bootstrapLaunchdJob(ctx context.Context, cmd *cobra.Command, verbose bool, domain, target, fullTargetName string, run runCmdFn) error {
	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	_, _ = run(ctx, "launchctl", "bootout", target) // best-effort: no-op if not currently loaded

	helpers.Debugf(cmd, verbose, "running: launchctl bootstrap %s %s", domain, fullTargetName)
	out, err := run(ctx, "launchctl", "bootstrap", domain, fullTargetName)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("bootstrap: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "bootstrap output: %s", strings.TrimSpace(string(out)))

	out, err = run(ctx, "launchctl", "enable", target)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "enable output: %s", strings.TrimSpace(string(out)))
	return nil
}

// resolveLaunchdPlistFile resolves the invoking user and renders the launchd
// plist file content for them, printing and wrapping errors from either step.
func resolveLaunchdPlistFile(cmd *cobra.Command, verbose bool, installDir, label string, userAgent bool) (*user.User, string, error) {
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return nil, "", helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "effective user: %s", effectiveUser.Username)

	plistFile, err := renderPlistFile(installDir, effectiveUser.Username, label, userAgent)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("rendering plist file: %v", err))
		return nil, "", helpers.ErrCommandFailed
	}
	return effectiveUser, plistFile, nil
}

// startupCmdLaunchd is the launchd (macOS) analog of startupCmd. Unlike systemd,
// launchd has no separate "load" step distinct from starting: RunAtLoad fires as soon
// as the job is bootstrapped. bootout is attempted first (best-effort, ignored if the
// job isn't loaded yet) so re-running this command is idempotent instead of failing
// with "service already bootstrapped".
// ensureLaunchdDir prepares the directory holding the plist: creating the
// per-user LaunchAgents dir, or validating the system LaunchDaemons dir.
func ensureLaunchdDir(cmd *cobra.Command, verbose, userAgent bool, launchdDir string) error {
	if userAgent {
		if err := os.MkdirAll(strings.TrimSuffix(launchdDir, "/"), 0750); err != nil {
			cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating LaunchAgents directory: %v", err))
			return helpers.ErrCommandFailed
		}
		helpers.Debugf(cmd, verbose, "ensured LaunchAgents directory: %s", launchdDir)
		return nil
	}
	if !prepareLaunchdTargetDir(cmd, launchdDir) {
		return helpers.ErrCommandFailed
	}
	return nil
}

func prepareLaunchdTargetDir(cmd *cobra.Command, dir string) bool {
	fileInfo, err := os.Stat(dir)
	if err != nil || !fileInfo.IsDir() {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", dir))
		return false
	}
	if err = checkWritable(cmd, dir); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return false
	}
	return true
}

type plistData struct {
	Label     string `json:"label"`
	ExecStart string `json:"exec_start"` // absolute path to eos binary
	User      string `json:"user"`
}

// renderPlistFile renders a launchd plist, the macOS analog of renderUnitFile.
// A system LaunchDaemon (userAgent=false) runs as root by default, so it pins UserName
// to the invoking user, mirroring the systemd system unit's User= line. A per-user
// LaunchAgent (userAgent=true) already runs as that user under their gui/<uid> session,
// so no UserName key is needed, mirroring the systemd user unit template.
func renderPlistFile(installDir string, user string, label string, userAgent bool) (string, error) {
	const systemPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>UserName</key>
	<string>{{.User}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.ExecStart}}</string>
		<string>daemon</string>
		<string>start</string>
		<string>--foreground</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`

	const userPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.ExecStart}}</string>
		<string>daemon</string>
		<string>start</string>
		<string>--foreground</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`

	tmplStr := systemPlistTemplate
	if userAgent {
		tmplStr = userPlistTemplate
	}

	tmpl, err := template.New("plist").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	data := plistData{
		Label:     label,
		ExecStart: filepath.Join(installDir, "eos"),
		User:      user,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	return buf.String(), nil
}

// offerEnableLinger explains why a user unit needs linger to survive logout
// and reboot, then offers to enable it right away — skipping the explanation
// entirely if linger is already on. A failed `loginctl enable-linger` is
// reported but non-fatal: startup still proceeds without it, same as if the
// user had declined.
func offerEnableLinger(ctx context.Context, cmd *cobra.Command, flagYes bool, username string, run runCmdFn) {
	if lingerEnabled(ctx, username, run) {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "linger already enabled — eos will also survive logout and reboot")
		return
	}

	cmd.Printf("%s\n", ui.TextMuted.Render("note: a user unit stops when you log out (SSH disconnect, desktop logout) — unlike a"))
	cmd.Printf(fmtHeading, ui.TextMuted.Render("system unit, it has no boot-time process to keep it alive once your session ends."))
	cmd.Printf(fmtHeading, ui.TextMuted.Render("\"linger\" tells systemd to keep your user session running with no one logged in."))

	declineMsg := fmt.Sprintf("linger not enabled — eos will stop when you log out; enable later with: %s",
		ui.TextCommand.Render("loginctl enable-linger "+username))
	if !confirmOrDecline(cmd, flagYes, "enable linger now so eos survives logout and reboot? (y/n):", declineMsg) {
		return
	}

	lingerOut, lingerErr := run(ctx, "loginctl", "enable-linger", username)
	if lingerErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("enable-linger: %v", string(lingerOut)))
		helpers.PrintSudoHint(cmd)
		cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), fmt.Sprintf("run manually: %s", ui.TextCommand.Render("loginctl enable-linger "+username)))
		return
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "linger enabled — eos will survive logout and reboot")
}

// lingerEnabled reports whether systemd linger is already enabled for username,
// so the enable-linger prompt in startupCmd isn't repeated on every run once
// accepted once. A loginctl failure (missing binary, older systemd, no
// polkit) is treated as "not enabled" — worst case the user is prompted
// again, never silently skipped.
func lingerEnabled(ctx context.Context, username string, run runCmdFn) bool {
	out, err := run(ctx, "loginctl", "show-user", username, "--property=Linger")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "Linger=yes"
}

// enableSystemdUnit runs daemon-reload then enable for the given unit scope.
func enableSystemdUnit(ctx context.Context, cmd *cobra.Command, verbose, userUnit bool, unit string, run runCmdFn) error {
	helpers.Debugf(cmd, verbose, logRunningSystemctl, strings.Join(systemctlArgs(userUnit, systemctlDaemonReload), " "))
	out, err := run(ctx, "systemctl", systemctlArgs(userUnit, systemctlDaemonReload)...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("daemon-reload: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "daemon-reload output: %s", strings.TrimSpace(string(out)))

	helpers.Debugf(cmd, verbose, logRunningSystemctl, strings.Join(systemctlArgs(userUnit, "enable", unit), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(userUnit, "enable", unit)...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "enable output: %s", strings.TrimSpace(string(out)))
	return nil
}

// ensureSystemdUnitDir prepares the directory that will hold the unit file:
// creating the user systemd dir, or validating/clearing the system dir.
func ensureSystemdUnitDir(cmd *cobra.Command, verbose, userUnit bool, systemdDir, fullTargetName string) error {
	if userUnit {
		if err := os.MkdirAll(strings.TrimSuffix(systemdDir, "/"), 0750); err != nil {
			cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating user systemd directory: %v", err))
			return helpers.ErrCommandFailed
		}
		helpers.Debugf(cmd, verbose, "ensured user systemd directory: %s", systemdDir)
		return nil
	}
	if !prepareSystemUnitDir(cmd, systemdDir, fullTargetName) {
		return helpers.ErrCommandFailed
	}
	return nil
}

func prepareSystemUnitDir(cmd *cobra.Command, systemdDir, fullTargetName string) bool {
	fileInfo, err := os.Stat(systemdDir)
	if err != nil || !fileInfo.IsDir() {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", systemdDir))
		return false
	}
	if err = checkWritable(cmd, systemdDir); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return false
	}
	existingUser := existingUnitUser(fullTargetName)
	if existingUser == "" {
		return true
	}
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		return true
	}
	if existingUser == effectiveUser.Username {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), fmt.Sprintf("system unit file already exists for user %q, re-running will overwrite and re-enable it", existingUser))
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), fmt.Sprintf("system unit file already configured for user %q, overwriting will transfer daemon ownership to %q and break their setup", existingUser, effectiveUser.Username))
		cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), fmt.Sprintf("run %s to remove the current startup config first, or ask user %q to do so", ui.TextCommand.Render(cmdnames.HintSystemUnstartup), existingUser))
	}
	return true
}

func existingUnitUser(unitFilePath string) string {
	data, err := os.ReadFile(unitFilePath) // #nosec G304 -- path is constructed internally
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "User="); ok {
			return v
		}
	}
	return ""
}
