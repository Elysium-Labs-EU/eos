package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/buildinfo"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/process"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

// supportedPlatforms lists the OS-arch combinations for which eos releases are published.
// Keep this in sync with the build pipeline.
var supportedPlatforms = []string{
	"linux-amd64",
	"linux-arm64",
}

func newSystemCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	var ctrl DaemonController // closed over by all subcommands below

	systemCmd := &cobra.Command{
		Use:   "system",
		Short: "Manage the eos system settings",
		Long:  `Manage eos system settings, check for updates, and inspect runtime configuration.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			_, baseDir, systemConfig, err := newSystemConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
				os.Exit(1)
			}
			ctrl, err = newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving daemon mode: %v", err))
				os.Exit(1)
			}
		},
	}

	infoCmd := &cobra.Command{
		Use:           "info",
		Short:         "See active system information and configurations",
		Long:          `Display active EOS configuration including install paths, daemon settings, health check limits, and shutdown grace period.`,
		Example:       `  eos system info`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			installDir, baseDir, config, err := newSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return helpers.ErrCommandFailed
			}
			infoCmd(cmd, installDir, baseDir, config)
			return nil
		},
	}

	startupCmdDef := &cobra.Command{
		Use:   "startup",
		Short: "Enable eos to start automatically on boot",
		Long: `Install a systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and enable it to run on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): installs a system unit at /etc/systemd/system/eos.service, a LaunchDaemon at /Library/LaunchDaemons/org.elysiumlabs.eos.plist on macOS, or a system-wide OpenRC init script at /etc/init.d/eos — one per host, daemon runs as the invoking user.
  - Run as a regular user: installs a user unit at ~/.config/systemd/user/eos.service, or a LaunchAgent at ~/Library/LaunchAgents/org.elysiumlabs.eos.plist on macOS — each user gets their own, no root required.

For systemd user units, add boot-time autostart (without login) with: loginctl enable-linger <username>

On OpenRC, installs a system-wide init script at /etc/init.d/eos and requires root — OpenRC has no per-user service scope.`,
		Example:       "  sudo eos system startup  # system unit (root, one per host)\n       eos system startup  # user unit (no root, per-user, systemd/launchd only)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			installDir, _, systemConfig, err := newSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return helpers.ErrCommandFailed
			}
			verbose, _ := cmd.Flags().GetBool("verbose")

			if runtime.GOOS == "darwin" {
				userAgent := os.Getuid() != 0
				launchdDir := config.LaunchdTargetDir
				if userAgent {
					launchdDir, err = config.UserLaunchAgentsDir()
					if err != nil {
						systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving user launch agents dir: %v", err))
						return helpers.ErrCommandFailed
					}
				}
				return startupCmdLaunchd(cmd.Context(), cmd, installDir, systemConfig.Daemon.Standalone,
					launchdDir, config.LaunchdPlistFileName, userAgent, verbose, execRunCmd)
			}

			runtimeName, err := detectActiveSystemRuntime()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
				return helpers.ErrCommandFailed
			}

			if runtimeName == "openrc" {
				return openrcStartupCmd(cmd.Context(), cmd, installDir, systemConfig.Daemon.Standalone,
					config.OpenRCInitDir, config.OpenRCTargetFileName,
					verbose, detectActiveSystemRuntime, execRunCmd)
			}

			userUnit := os.Getuid() != 0
			systemdDir := config.SystemdTargetDir
			if userUnit {
				systemdDir, err = config.UserSystemdDir()
				if err != nil {
					systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving user systemd dir: %v", err))
					return helpers.ErrCommandFailed
				}
			}
			return startupCmd(cmd.Context(), cmd, installDir, systemConfig.Daemon.Standalone,
				systemdDir, config.SystemdTargetFileName,
				userUnit, verbose, detectActiveSystemRuntime, execRunCmd)
		},
	}

	unstartupCmdDef := &cobra.Command{
		Use:   "unstartup",
		Short: "Disable eos from starting automatically on boot",
		Long: `Remove the systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and disable it from running on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): removes the system unit / LaunchDaemon / OpenRC init script.
  - Run as a regular user: removes the user unit / LaunchAgent.

On OpenRC, removes the system-wide init script at /etc/init.d/eos and requires root.`,
		Example:       "  sudo eos system unstartup  # remove system unit\n       eos system unstartup  # remove user unit (systemd/launchd only)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, systemConfig, err := newSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return helpers.ErrCommandFailed
			}
			verbose, _ := cmd.Flags().GetBool("verbose")

			if runtime.GOOS == "darwin" {
				if systemConfig.Daemon.Launchd == nil {
					systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no launchd startup configured for this user — nothing to remove")
					return helpers.ErrCommandFailed
				}
				userAgent := os.Getuid() != 0
				return unstartupCmdLaunchd(cmd.Context(), cmd, *systemConfig.Daemon.Launchd, userAgent, verbose, execRunCmd)
			}

			runtimeName, err := detectActiveSystemRuntime()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
				return helpers.ErrCommandFailed
			}

			if runtimeName == "openrc" {
				return openrcUnstartupCmd(cmd.Context(), cmd, config.OpenRCInitDir, config.OpenRCTargetFileName,
					verbose, detectActiveSystemRuntime, execRunCmd)
			}

			if systemConfig.Daemon.Systemd == nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no systemd startup configured for this user — nothing to remove")
				return helpers.ErrCommandFailed
			}
			userUnit := os.Getuid() != 0
			return unstartupCmd(cmd.Context(), cmd, *systemConfig.Daemon.Systemd, userUnit, verbose, detectActiveSystemRuntime, execRunCmd)
		},
	}

	updateCmd := &cobra.Command{
		Use:           "update",
		Short:         "Apply new update if available",
		Long:          `Check Codeberg for a newer eos release and optionally download and install it. Uses SHA256 checksum validation and backs up the current binary before replacing it.`,
		Example:       "  eos system update        # check and apply latest stable release\n  eos system update --pre  # include pre-releases",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			installDir, _, _, err := newSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return helpers.ErrCommandFailed
			}
			includePre, err := cmd.Flags().GetBool("pre")
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return helpers.ErrCommandFailed
			}

			// Overrides for testing purposes
			version := buildinfo.GetVersionOnly()
			if override := os.Getenv("EOS_VERSION"); override != "" {
				version = override
			}
			userArch := runtime.GOARCH
			if override := os.Getenv("USER_ARCH"); override != "" {
				userArch = override
			}
			userOS := runtime.GOOS
			if override := os.Getenv("USER_OS"); override != "" {
				userOS = override
			}
			return updateCmd(cmd.Context(), cmd, version, installDir, ctrl, userArch, userOS, includePre, fetchLatestRelease, handleDownloadBinary, fetchChecksumForBinary)
		},
	}
	updateCmd.Flags().Bool("pre", false, "includes pre-releases in update check")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove eos from this system",
		Long:  `Stops all running services, removes the eos binary and configuration, and cleans up the install directory. Prompts for confirmation unless --yes is passed.`,
		Example: `  eos system uninstall        # interactive uninstall with confirmation prompt
  eos system uninstall --yes  # skip confirmation (non-interactive)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			installDir, baseDir, _, err := newSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return helpers.ErrCommandFailed
			}

			flagYes, err := cmd.Flags().GetBool("yes")
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return helpers.ErrCommandFailed
			}

			if !flagYes {
				confirmed := helpers.PromptConfirm(cmd, "uninstall eos? (y/n):")
				if !confirmed {
					systemCmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "uninstall canceled")
					return nil
				}
			}
			return uninstallCmd(cmd, getManager, getConfig, ctrl, installDir, baseDir, flagYes)
		},
	}
	uninstallCmd.Flags().BoolP("yes", "y", false, "skip all confirmation prompts (non-interactive mode)")

	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "Get version of system",
		Long:    `Print the current eos version, git commit hash, and build date.`,
		Example: `  eos system version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println(buildinfo.Get())
			return nil
		},
	}

	systemCmd.AddCommand(infoCmd)
	systemCmd.AddCommand(startupCmdDef)
	systemCmd.AddCommand(unstartupCmdDef)
	systemCmd.AddCommand(updateCmd)
	systemCmd.AddCommand(uninstallCmd)
	systemCmd.AddCommand(versionCmd)

	return systemCmd
}

func infoCmd(cmd *cobra.Command, installDir string, baseDir string, config *config.SystemConfig) {
	cmd.Println()
	cmd.Printf("%s\n\n", ui.TextBold.Render("System Config"))
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("install dir:"), installDir)
	cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("base dir:"), baseDir)
	cmd.Printf("%s\n\n", ui.TextBold.Render("Daemon"))
	if config.Daemon.Standalone != nil {
		cmd.Printf("  %s %v\n\n", ui.TextMuted.Render("systemd managed:"), false)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("pid file:"), config.Daemon.Standalone.PIDFile)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket:"), config.Daemon.Standalone.SocketPath)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket timeout:"), config.Daemon.Standalone.SocketTimeout)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("log dir:"), config.Daemon.Standalone.Log.LogDir)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("log file:"), config.Daemon.Standalone.Log.LogFileName)
		cmd.Printf("  %s %d\n", ui.TextMuted.Render("log max files:"), config.Daemon.Standalone.Log.LogMaxFiles)
		cmd.Printf("  %s %d\n\n", ui.TextMuted.Render("log size limit:"), config.Daemon.Standalone.Log.LogFileSizeLimit)
	} else {
		cmd.Printf("  %s %v\n\n", ui.TextMuted.Render("systemd managed:"), true)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("systemd target directory:"), config.Daemon.Systemd.SystemdTargetDir)
		cmd.Printf("  %s %s\n", ui.TextMuted.Render("systemd target filename:"), config.Daemon.Systemd.SystemdTargetFileName)
	}
	cmd.Printf("%s\n\n", ui.TextBold.Render("Health Check"))
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("max restarts:"), config.Health.MaxRestart)
	cmd.Printf("  %s %v\n", ui.TextMuted.Render("timeout enabled:"), config.Health.Timeout.Enable)
	if config.Health.Timeout.Enable {
		cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit)
	} else {
		cmd.Printf("  %s %s %s\n\n", ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit, ui.TextMuted.Render("(not active)"))
	}
	cmd.Printf("%s\n\n", ui.TextBold.Render("Shutdown"))
	cmd.Printf("  %s %v\n", ui.TextMuted.Render("grace period:"), config.Shutdown.GracePeriod)
}

// detectActiveSystemRuntime identifies the running init system by checking for
// well-known markers rather than trusting /proc/1/comm, which is unreliable
// inside containers and PID namespaces where PID 1 isn't the real init.
// /run/systemd/system is the canonical systemd-is-running check (see
// sd_booted(3)); /sbin/openrc is OpenRC's control binary, present whenever
// OpenRC manages the system (Alpine's default, among others).
func detectActiveSystemRuntime() (string, error) {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return "systemd", nil
	}
	if _, err := os.Stat("/sbin/openrc"); err == nil {
		return "openrc", nil
	}
	return "unknown", nil
}

type unitData struct {
	ExecStart string `json:"exec_start"` // absolute path to eos binary
	PIDFile   string `json:"pid_file"`   // absolute path to eos.pid
	User      string `json:"user"`
}

func renderUnitFile(installDir string, user string, userUnit bool) (string, error) {
	const systemUnitTemplate = `[Unit]
Description=eos deployment daemon
After=network.target

[Service]
Type=simple
ExecStart={{.ExecStart}} daemon start
Restart=always
RestartSec=5s
User={{.User}}

[Install]
WantedBy=multi-user.target`

	const userUnitTemplate = `[Unit]
Description=eos deployment daemon
After=network.target

[Service]
Type=simple
ExecStart={{.ExecStart}} daemon start
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

type runCmdFn func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- name is always "systemctl"
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

func unitScope(userUnit bool) string {
	if userUnit {
		return "user unit"
	}
	return "system unit"
}

// unitName derives the systemctl unit name (e.g. "eos") from a unit filename
// (e.g. "eos.service"). Production always uses config.SystemdTargetFileName
// ("eos.service" -> "eos"); tests pass an isolated name so systemctl
// enable/disable/stop calls target a throwaway unit instead of the real one.
func unitName(systemdFile string) string {
	return strings.TrimSuffix(systemdFile, ".service")
}

func prepareSystemUnitDir(cmd *cobra.Command, systemdDir, fullTargetName string) bool {
	fileInfo, err := os.Stat(systemdDir)
	if err != nil || !fileInfo.IsDir() {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", systemdDir))
		return false
	}
	if err = checkWritable(cmd, systemdDir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
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
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("system unit file already exists for user %q, re-running will overwrite and re-enable it", existingUser))
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("system unit file already configured for user %q, overwriting will transfer daemon ownership to %q and break their setup", existingUser, effectiveUser.Username))
		cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), fmt.Sprintf("run %s to remove the current startup config first, or ask user %q to do so", ui.TextCommand.Render("eos system unstartup"), existingUser))
	}
	return true
}

func systemctlArgs(userUnit bool, args ...string) []string {
	if userUnit {
		return append([]string{"--user"}, args...)
	}
	return args
}

// userRuntimeDir returns the systemd user runtime dir for uid, e.g. /run/user/1000. uid must be
// the target user's uid, not necessarily os.Getuid() — under sudo, os.Getuid() is 0 (root) while
// the systemd --user session being managed belongs to userutil.EffectiveUser().
func userRuntimeDir(uid int) string {
	return fmt.Sprintf("/run/user/%d", uid)
}

// isAccessibleDir reports whether path is a directory owned by uid. Ownership matters here, not
// just stat-ability: /run/user is world-traversable (0755), so stat succeeds on any uid's runtime
// dir even though its 0700 permissions block everything else; a stale XDG_RUNTIME_DIR pointing at
// another user's dir would otherwise look "accessible" and never get corrected, later failing with
// "Failed to connect to bus: Permission denied". uid is the target user's uid (see userRuntimeDir),
// not necessarily os.Getuid() — comparing against os.Getuid() would wrongly reject a sudo-invoking
// user's own runtime dir when root manages that user's systemd --user session.
func isAccessibleDir(path string, uid int) bool {
	fileInfo, err := os.Stat(path) // #nosec G703 -- path is either the user's own XDG_RUNTIME_DIR or a derived /run/user/<uid>, never external input
	if err != nil {
		return false
	}
	if !fileInfo.IsDir() {
		return false
	}
	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(stat.Uid) == uid
}

// ensureUserBusAvailable diagnoses and, where possible, auto-fixes the "no systemd user bus"
// condition that causes `systemctl --user ...` to fail with "Failed to connect to bus: Permission
// denied". This happens when XDG_RUNTIME_DIR is unset/stale (fixable by correcting the env var) or
// when the account has no active session and no linger enabled (fixable via `loginctl enable-linger`).
// expected is the runtime dir this process should be using (userRuntimeDir(uid) in production;
// injected directly in tests so they don't depend on the real /run/user/<uid>). uid is the target
// user's uid — the user the systemd --user session belongs to, resolved via
// userutil.EffectiveUser() by the caller, not necessarily os.Getuid() (root under sudo).
func ensureUserBusAvailable(ctx context.Context, cmd *cobra.Command, verbose bool, username string, uid int, expected string, run runCmdFn) error {
	current := os.Getenv("XDG_RUNTIME_DIR")
	helpers.Debugf(cmd, verbose, "XDG_RUNTIME_DIR=%q (expected %q)", current, expected)

	if isAccessibleDir(current, uid) {
		return nil
	}

	if isAccessibleDir(expected, uid) {
		helpers.Debugf(cmd, verbose, "correcting XDG_RUNTIME_DIR to %q", expected)
		if err := os.Setenv("XDG_RUNTIME_DIR", expected); err != nil {
			return fmt.Errorf("setting XDG_RUNTIME_DIR: %w", err)
		}
		return nil
	}

	cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "no active systemd user session found — user bus is not running")
	cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), "this happens when the account has no login session and linger is not enabled")

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("enable linger for %q to start a user bus now? (y/n):", username))
	if !confirmed {
		return fmt.Errorf("no user bus available and linger was not enabled")
	}

	out, err := run(ctx, "loginctl", "enable-linger", username)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("enable-linger: %v", string(out)))
		helpers.PrintSudoHint(cmd)
		cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), fmt.Sprintf("run manually: %s", ui.TextCommand.Render("sudo loginctl enable-linger "+username)))
		return fmt.Errorf("enabling linger: %w", err)
	}
	helpers.Debugf(cmd, verbose, "loginctl enable-linger %s succeeded", username)

	for attempt := 1; attempt <= 5; attempt++ {
		helpers.Debugf(cmd, verbose, "checking for %q (attempt %d/5)", expected, attempt)
		if isAccessibleDir(expected, uid) {
			if err := os.Setenv("XDG_RUNTIME_DIR", expected); err != nil {
				return fmt.Errorf("setting XDG_RUNTIME_DIR: %w", err)
			}
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "user bus is now available")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("user bus still unavailable after enabling linger — a fresh login may be required")
}

func startupCmd(ctx context.Context, cmd *cobra.Command, installDir string, daemonConfig *config.StandaloneDaemonConfig, systemdDir, systemdFile string, userUnit, verbose bool, detectRuntime func() (string, error), run runCmdFn) error { //nolint:unparam // systemdFile drives the systemctl unit name; varies in integration tests (excluded by build tag)
	runtime, err := detectRuntime()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "detected runtime: %s", runtime)
	if runtime != "systemd" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("managing startup file not supported for this runtime: %v", runtime))
		return helpers.ErrCommandFailed
	}

	fullTargetName := filepath.Join(systemdDir, systemdFile)
	helpers.Debugf(cmd, verbose, "target unit file: %s", fullTargetName)

	if userUnit {
		if err = os.MkdirAll(strings.TrimSuffix(systemdDir, "/"), 0750); err != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating user systemd directory: %v", err))
			return helpers.ErrCommandFailed
		}
		helpers.Debugf(cmd, verbose, "ensured user systemd directory: %s", systemdDir)
	} else if !prepareSystemUnitDir(cmd, systemdDir, fullTargetName) {
		return helpers.ErrCommandFailed
	}

	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "effective user: %s", effectiveUser.Username)

	unitFile, err := renderUnitFile(installDir, effectiveUser.Username, userUnit)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("rendering unit file: %v", err))
		return helpers.ErrCommandFailed
	}

	unitKind := unitScope(userUnit) + " file"

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("create %s? (y/n):", unitKind))
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), unitKind+" creation canceled")
		return nil
	}

	err = os.WriteFile(fullTargetName, []byte(unitFile), 0644) // #nosec G306 -- unit files should be readable by other users/tools
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing unit file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(unitKind+" created, at:"), fullTargetName)

	if userUnit {
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
			return helpers.ErrCommandFailed
		}
		err = ensureUserBusAvailable(ctx, cmd, verbose, effectiveUser.Username, int(effectiveUID), userRuntimeDir(int(effectiveUID)), run)
		if err != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("preparing user bus: %v", err))
			return helpers.ErrCommandFailed
		}
	}

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "daemon-reload"), " "))
	out, err := run(ctx, "systemctl", systemctlArgs(userUnit, "daemon-reload")...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("daemon-reload: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "daemon-reload output: %s", strings.TrimSpace(string(out)))

	unit := unitName(systemdFile)
	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "enable", unit), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(userUnit, "enable", unit)...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "enable output: %s", strings.TrimSpace(string(out)))

	if userUnit {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "user unit enabled, eos will start on login")
		cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), fmt.Sprintf("to also start at boot without login: %s", ui.TextCommand.Render("loginctl enable-linger "+effectiveUser.Username)))
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "system unit enabled, eos will start on boot")
	}

	confirmed = helpers.PromptConfirm(cmd, "restart daemon now? (y/n):")
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon will be managed by systemd on next start")
		return nil
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
	if daemonConfig == nil {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
	} else {
		killed, killErr := process.StopStandaloneDaemon(daemonConfig.PIDFile, daemonConfig.SocketPath)
		if killErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
			return helpers.ErrCommandFailed
		}
		if !killed {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
		} else {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")
		}
	}

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "start", unit), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(userUnit, "start", unit)...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting systemd daemon: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "start output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

func unstartupCmd(ctx context.Context, cmd *cobra.Command, daemonConfig config.SystemdConfig, userUnit, verbose bool, detectRuntime func() (string, error), run runCmdFn) error {
	runtime, err := detectRuntime()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "detected runtime: %s", runtime)
	if runtime != "systemd" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("managing startup file not supported for this runtime: %v", runtime))
		return helpers.ErrCommandFailed
	}

	unitKind := unitScope(userUnit)

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("remove %s and disable eos on boot? (y/n):", unitKind))
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "canceled")
		return nil
	}

	if userUnit {
		effectiveUser, effectiveUserErr := userutil.EffectiveUser()
		if effectiveUserErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
			return helpers.ErrCommandFailed
		}
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
			return helpers.ErrCommandFailed
		}
		err = ensureUserBusAvailable(ctx, cmd, verbose, effectiveUser.Username, int(effectiveUID), userRuntimeDir(int(effectiveUID)), run)
		if err != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("preparing user bus: %v", err))
			return helpers.ErrCommandFailed
		}
	}

	unit := unitName(daemonConfig.SystemdTargetFileName)
	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "stop", unit), " "))
	out, err := run(ctx, "systemctl", systemctlArgs(userUnit, "stop", unit)...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping %s: %v", unitKind, string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), unitKind+" stopped")

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "disable", unit), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(userUnit, "disable", unit)...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("disabling %s: %v", unitKind, string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), unitKind+" disabled")

	err = os.Remove(daemonConfig.SystemdTargetDir + daemonConfig.SystemdTargetFileName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing unit file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "unit file removed")

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(systemctlArgs(userUnit, "daemon-reload"), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(userUnit, "daemon-reload")...)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("daemon-reload: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), unitKind+" startup removed")

	if userUnit {
		cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), "if you enabled linger, also run: loginctl disable-linger <username>")
	}

	confirmed = helpers.PromptConfirm(cmd, "restart daemon standalone? (y/n):")
	if !confirmed {
		return nil
	}

	if err := forkDaemon(ctx, config.DaemonPIDFile, false); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos daemon logs") + ui.TextMuted.Render(" → check daemon logs") + "\n")
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
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

// launchdLabel derives the launchctl job label (e.g. "org.elysiumlabs.eos") from a
// plist filename (e.g. "org.elysiumlabs.eos.plist"). Mirrors unitName for systemd;
// tests pass an isolated name so launchctl calls target a throwaway job.
func launchdLabel(plistFileName string) string {
	return strings.TrimSuffix(plistFileName, ".plist")
}

// launchdDomain returns the launchctl target domain: "system" for a LaunchDaemon,
// or "gui/<uid>" for a LaunchAgent running in the given user's GUI session.
func launchdDomain(userAgent bool, uid int) string {
	if userAgent {
		return fmt.Sprintf("gui/%d", uid)
	}
	return "system"
}

func launchdScope(userAgent bool) string {
	if userAgent {
		return "launch agent"
	}
	return "launch daemon"
}

func prepareLaunchdTargetDir(cmd *cobra.Command, dir string) bool {
	fileInfo, err := os.Stat(dir)
	if err != nil || !fileInfo.IsDir() {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", dir))
		return false
	}
	if err = checkWritable(cmd, dir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return false
	}
	return true
}

// startupCmdLaunchd is the launchd (macOS) analog of startupCmd. Unlike systemd,
// launchd has no separate "load" step distinct from starting: RunAtLoad fires as soon
// as the job is bootstrapped. bootout is attempted first (best-effort, ignored if the
// job isn't loaded yet) so re-running this command is idempotent instead of failing
// with "service already bootstrapped".
func startupCmdLaunchd(ctx context.Context, cmd *cobra.Command, installDir string, daemonConfig *config.StandaloneDaemonConfig, launchdDir, plistFileName string, userAgent, verbose bool, run runCmdFn) error {
	fullTargetName := filepath.Join(launchdDir, plistFileName)
	helpers.Debugf(cmd, verbose, "target plist file: %s", fullTargetName)

	if userAgent {
		if err := os.MkdirAll(strings.TrimSuffix(launchdDir, "/"), 0750); err != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating LaunchAgents directory: %v", err))
			return helpers.ErrCommandFailed
		}
		helpers.Debugf(cmd, verbose, "ensured LaunchAgents directory: %s", launchdDir)
	} else if !prepareLaunchdTargetDir(cmd, launchdDir) {
		return helpers.ErrCommandFailed
	}

	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "effective user: %s", effectiveUser.Username)

	label := launchdLabel(plistFileName)
	plistFile, err := renderPlistFile(installDir, effectiveUser.Username, label, userAgent)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("rendering plist file: %v", err))
		return helpers.ErrCommandFailed
	}

	plistKind := launchdScope(userAgent) + " file"

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("create %s? (y/n):", plistKind))
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), plistKind+" creation canceled")
		return nil
	}

	err = os.WriteFile(fullTargetName, []byte(plistFile), 0644) // #nosec G306 -- plist files should be readable by other users/tools
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing plist file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(plistKind+" created, at:"), fullTargetName)

	uid := os.Getuid()
	if userAgent {
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
			return helpers.ErrCommandFailed
		}
		uid = int(effectiveUID)
	}
	domain := launchdDomain(userAgent, uid)
	target := domain + "/" + label

	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	_, _ = run(ctx, "launchctl", "bootout", target) // best-effort: no-op if not currently loaded

	helpers.Debugf(cmd, verbose, "running: launchctl bootstrap %s %s", domain, fullTargetName)
	out, err := run(ctx, "launchctl", "bootstrap", domain, fullTargetName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("bootstrap: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "bootstrap output: %s", strings.TrimSpace(string(out)))

	out, err = run(ctx, "launchctl", "enable", target)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "enable output: %s", strings.TrimSpace(string(out)))

	if userAgent {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "launch agent enabled, eos will start on login")
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "launch daemon enabled, eos will start on boot")
	}

	confirmed = helpers.PromptConfirm(cmd, "restart daemon now? (y/n):")
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon will be managed by launchd on next start")
		return nil
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
	if daemonConfig == nil {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
	} else {
		killed, killErr := process.StopStandaloneDaemon(daemonConfig.PIDFile, daemonConfig.SocketPath)
		if killErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
			return helpers.ErrCommandFailed
		}
		if !killed {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
		} else {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")
		}
	}

	helpers.Debugf(cmd, verbose, "running: launchctl kickstart -k %s", target)
	out, err = run(ctx, "launchctl", "kickstart", "-k", target)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting launchd daemon: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "kickstart output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

// unstartupCmdLaunchd is the launchd (macOS) analog of unstartupCmd. "launchctl
// bootout" both stops the job and unloads it in one step (the combined equivalent of
// "systemctl stop" + "systemctl disable"): the plist stays on disk until removed below,
// but won't be re-bootstrapped until the next "eos system startup", boot, or login.
func unstartupCmdLaunchd(ctx context.Context, cmd *cobra.Command, daemonConfig config.LaunchdConfig, userAgent, verbose bool, run runCmdFn) error {
	scopeKind := launchdScope(userAgent)

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("remove %s and disable eos on boot? (y/n):", scopeKind))
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "canceled")
		return nil
	}

	uid := os.Getuid()
	if userAgent {
		effectiveUser, effectiveUserErr := userutil.EffectiveUser()
		if effectiveUserErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
			return helpers.ErrCommandFailed
		}
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
			return helpers.ErrCommandFailed
		}
		uid = int(effectiveUID)
	}
	domain := launchdDomain(userAgent, uid)
	label := launchdLabel(daemonConfig.LaunchdPlistFileName)
	target := domain + "/" + label

	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	out, err := run(ctx, "launchctl", "bootout", target)
	if err != nil {
		// Unlike "systemctl stop" (idempotent, exits 0 on an already-stopped unit),
		// "launchctl bootout" exits 3 ("No such process") when the job isn't currently
		// loaded — verified empirically. Treat that as already-stopped rather than a
		// fatal error, or "eos system unstartup" would hard-fail and never remove the
		// plist whenever the job happened to already be stopped.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			helpers.Debugf(cmd, verbose, "launchctl bootout: job was not loaded")
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(scopeKind+" was not loaded"))
		} else {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping %s: %v", scopeKind, string(out)))
			return helpers.ErrCommandFailed
		}
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), scopeKind+" stopped and unloaded")
	}

	err = os.Remove(filepath.Join(daemonConfig.LaunchdTargetDir, daemonConfig.LaunchdPlistFileName))
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing plist file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), scopeKind+" startup removed")

	confirmed = helpers.PromptConfirm(cmd, "restart daemon standalone? (y/n):")
	if !confirmed {
		return nil
	}

	if err := forkDaemon(ctx, config.DaemonPIDFile, false); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos daemon logs") + ui.TextMuted.Render(" → check daemon logs") + "\n")
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

func updateCmd(ctx context.Context, cmd *cobra.Command, version string, installDir string, ctrl DaemonController, userArch string, userOS string, includePre bool, fetchRelease func(context.Context, bool) (*Release, error), downloadBinary func(context.Context, *Asset) (*os.File, string, error), getChecksum func(context.Context, *Asset, string) (string, error)) error {
	binaryPath := filepath.Join(installDir, "eos")

	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "checking for updates...")

	fileInfo, err := os.Stat(installDir)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", installDir))
		return helpers.ErrCommandFailed
	}

	if !fileInfo.IsDir() {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", installDir))
		return helpers.ErrCommandFailed
	}

	err = checkWritable(cmd, installDir)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}

	if version == "dev" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "updating not supported for dev builds")
		return helpers.ErrCommandFailed
	}

	if !strings.HasPrefix(version, "v") {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid version tag, must start with 'v'")
		return helpers.ErrCommandFailed
	}

	if !semver.IsValid(version) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid semantic version")
		return helpers.ErrCommandFailed
	}

	release, err := fetchRelease(ctx, includePre)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("fetching latest release: %v", err))
		return helpers.ErrCommandFailed
	}

	result, err := checkForUpdates(release, version, userArch, userOS)
	latestVersion := result.LatestVersion
	latestAsset := result.Asset

	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("no compatible asset found for %s-%s", userOS, userArch))
		cmd.PrintErrf("  %s %s\n\n", ui.TextMuted.Render("supported platforms:"), strings.Join(supportedPlatforms, ", "))
		return helpers.ErrCommandFailed
	}
	if latestVersion == "" {
		cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), "already on the latest version", ui.TextMuted.Render(fmt.Sprintf("(%s)", version)))
		return nil
	}

	cmd.Printf("%s %s → %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(version), ui.TextBold.Render(latestVersion))
	confirmed := helpers.PromptConfirm(cmd, "upgrade? (y/n):")

	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "update canceled")
		return nil
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), fmt.Sprintf("downloading eos %s for %s-%s...", latestVersion, userOS, userArch))
	binary, tempDir, err := downloadBinary(ctx, latestAsset)

	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("downloading binary: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "validating checksums...")
	expectedChecksum, err := getChecksum(ctx, result.ChecksumsAsset, latestAsset.Name)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("fetching checksums: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return helpers.ErrCommandFailed
	}
	err = validateDigest(expectedChecksum, binary)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checksum validation failed: %v", err))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos system update") + ui.TextMuted.Render(" → retry the update") + "\n")
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "checksums match")

	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, time.Now().Format("20060102_150405"))
	err = createDestinationFile(backupPath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating destination file: %v", err))
	}

	if err := copyFile(binaryPath, backupPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("backing up current binary: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("info"), "backup created at", ui.TextMuted.Render(backupPath))

	if err := replaceBinary(binary.Name(), binaryPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("installing new binary: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return helpers.ErrCommandFailed
	}
	if err := os.Chmod(binaryPath, 0755); err != nil { // #nosec G302 -- executable binary needs to be runnable by all users
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("setting permissions: %v", err))
		return helpers.ErrCommandFailed
	}

	refreshInstalledCompletions(ctx, cmd, binaryPath)

	confirmed = helpers.PromptConfirm(cmd, "restart daemon? (y/n):")

	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "manual daemon restart required")
		cmd.Printf("\n%s %s %s\n\n", ui.LabelSuccess.Render("success"), "eos updated to", ui.TextBold.Render(latestVersion))
		return nil
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	killed, killErr := ctrl.Stop(ctx, cmd, verbose)
	if killErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
		return helpers.ErrCommandFailed
	}
	if !killed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
		return nil
	}

	if err := ctrl.Start(ctx, true, false, false); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(ctrl.LogsHint()) + ui.TextMuted.Render(" → check daemon logs") + "\n")
		return helpers.ErrCommandFailed
	}

	if err := os.RemoveAll(tempDir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, err))
	}

	cmd.Printf("\n%s %s %s\n", ui.LabelSuccess.Render("success"), "eos updated to", ui.TextBold.Render(latestVersion))
	if os.Getuid() == 0 {
		cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("this only restarted the invoking user's daemon — other users on this host may still be running the pre-update binary"))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos daemon info --all") + ui.TextMuted.Render(" → check every user's daemon") + "\n")
	}
	return nil
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

func fetchLatestRelease(ctx context.Context, includePre bool) (*Release, error) {
	url := "https://codeberg.org/api/v1/repos/Elysium_Labs/eos/releases/latest"
	if includePre {
		url = "https://codeberg.org/api/v1/repos/Elysium_Labs/eos/releases"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request building failed: %w", err)
	}

	// #nosec G704
	resp, err := httpClient.Do(req)
	defer func() {
		if resp == nil {
			return
		}
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing response body: %w", closeErr)
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	if includePre {
		var releases []Release
		if err = json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		if len(releases) == 0 {
			return nil, fmt.Errorf("no releases found")
		}
		return &releases[0], nil
	}

	var release Release
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &release, nil
}

type UpdateResult struct {
	Asset          *Asset
	ChecksumsAsset *Asset
	LatestVersion  string
}

func checkForUpdates(release *Release, current string, arch string, os string) (result UpdateResult, err error) {
	latest := release.TagName

	if semver.Compare(current, latest) >= 0 {
		return UpdateResult{}, nil
	}

	var usableAsset *Asset
	var checksumsAsset *Asset
	for i, asset := range release.Assets {
		if strings.Contains(asset.Name, arch) && strings.Contains(asset.Name, os) {
			usableAsset = &release.Assets[i]
		}
		if asset.Name == "sha256sums.txt" {
			checksumsAsset = &release.Assets[i]
		}
	}

	if usableAsset == nil {
		return UpdateResult{}, fmt.Errorf("no usable asset found")
	}

	return UpdateResult{Asset: usableAsset, ChecksumsAsset: checksumsAsset, LatestVersion: latest}, nil
}

func handleDownloadBinary(ctx context.Context, latestAsset *Asset) (_ *os.File, tempDir string, err error) {
	parsedURL, err := url.Parse(latestAsset.BrowserDownloadURL)
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Hostname(), "codeberg.org") {
		return nil, "", fmt.Errorf("invalid URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAsset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("request building failed: %w", err)
	}

	resp, err := httpClient.Do(req) // #nosec G704 -- URL is constructed from hardcoded GitHub API base, not user input
	defer func() {
		if resp == nil {
			return
		}
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing response body: %w", closeErr)
		}
	}()
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	if resp == nil {
		return nil, "", fmt.Errorf("response is nil")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	cleanUpRequiredOnError := true
	tempDir, err = os.MkdirTemp("", "tempDownloadDir")
	if err != nil {
		return nil, "", fmt.Errorf("unable to create temporary download directory for downloading binary: %w", err)
	}
	defer func() {
		if cleanUpRequiredOnError {
			if cleanUpErr := os.RemoveAll(tempDir); cleanUpErr != nil {
				if err != nil {
					err = fmt.Errorf("%w; cleanup also failed: %w", err, cleanUpErr)
				} else {
					err = fmt.Errorf("cleaning up temporary directory: %w", cleanUpErr)
				}
			}
		}
	}()

	file, err := os.Create(filepath.Clean(filepath.Join(tempDir, latestAsset.Name)))
	if err != nil {
		return nil, "", fmt.Errorf("errored during creating file for downloading binary: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("errored during copying contents of fetched binary: %w", err)
	}

	if resp.ContentLength != -1 && written != resp.ContentLength {
		return nil, "", fmt.Errorf("received file doesn't match expected size")
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, "", fmt.Errorf("failed to reset seeker on the file: %w", err)
	}

	cleanUpRequiredOnError = false
	return file, tempDir, nil
}

func checkWritable(cmd *cobra.Command, dir string) error {
	file, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("directory %q does not appear to be writable: %w", dir, err)
	}

	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("closing temp file: %w", closeErr)
	}

	if removeErr := os.Remove(file.Name()); removeErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("could not remove temp file %s: %v\n", file.Name(), removeErr))
	}

	return nil
}

func fetchChecksumForBinary(ctx context.Context, checksumsAsset *Asset, binaryName string) (string, error) {
	if checksumsAsset == nil {
		return "", fmt.Errorf("no sha256sums.txt asset in release")
	}

	parsedURL, err := url.Parse(checksumsAsset.BrowserDownloadURL)
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Hostname(), "codeberg.org") {
		return "", fmt.Errorf("invalid checksums URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsAsset.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching sha256sums.txt: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("nil response fetching sha256sums.txt")
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response, close error not actionable
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status fetching sha256sums.txt: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == binaryName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading sha256sums.txt: %w", err)
	}

	return "", fmt.Errorf("no checksum found for %q in sha256sums.txt", binaryName)
}

func validateDigest(expectedChecksum string, binary *os.File) error {
	if _, err := binary.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to reset seeker on the file: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, binary); err != nil {
		return fmt.Errorf("failed to hash binary: %w", err)
	}
	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if expectedChecksum != calculatedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculatedChecksum)
	}

	return nil
}

func copyFile(src string, dst string) (err error) {
	source, err := os.Open(filepath.Clean(src)) // #nosec G703 -- src is constructed internally, not from user input
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if sourceCloseErr := source.Close(); sourceCloseErr != nil && err == nil {
			err = fmt.Errorf("errored closing the source file: %w", sourceCloseErr)
		}
	}()

	destination, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_TRUNC, 0644) // #nosec G302 -- backup file should be readable by all users
	if err != nil {
		return fmt.Errorf("failed to open destination file: %w", err)
	}
	defer func() {
		if destinationCloseErr := destination.Close(); destinationCloseErr != nil && err == nil {
			err = fmt.Errorf("errored closing the destination file: %w", destinationCloseErr)
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(dst); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = fmt.Errorf("failed to remove partial destination file: %w", removeErr)
			}
		}
	}()

	return nil
}

func createDestinationFile(dst string) error {
	destination, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}
	defer func() {
		if destinationErr := destination.Close(); destinationErr != nil && err == nil {
			err = fmt.Errorf("closing destination file: %w", destinationErr)
		}
	}()
	return nil
}

func replaceBinary(src string, dst string) (err error) {
	source, err := os.Open(filepath.Clean(src)) // #nosec G703 -- src is constructed internally, not from user input
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("errored closing the source file: %w", closeErr)
		}
	}()

	tmpDst := dst + ".tmp"

	destination, err := os.Create(filepath.Clean(tmpDst))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = destination.Close()
			_ = os.Remove(tmpDst)
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	if err = destination.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err = os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return nil
}

func uninstallCmd(cmd *cobra.Command, getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, ctrl DaemonController, installDir string, baseDir string, flagYes bool) error {
	mgr := getManager()
	cfg := getConfig()

	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "checking for active services...")

	serviceInstances, err := mgr.GetAllServiceInstances()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting all service instances: %v", err))
		return helpers.ErrCommandFailed
	}

	numberActiveServices := len(serviceInstances)
	if numberActiveServices == 1 {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("found %d active service", numberActiveServices))
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("found %d active services", numberActiveServices))
	}

	if numberActiveServices > 0 {
		stopAllServices := handleStoppingServices(cmd, mgr, cfg, serviceInstances, flagYes)
		if !stopAllServices {
			return nil
		}
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
	verbose, _ := cmd.Flags().GetBool("verbose")
	_, err = ctrl.Stop(cmd.Context(), cmd, verbose)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")

	binaryRemoveErr := os.Remove(filepath.Join(installDir, "eos"))
	if binaryRemoveErr != nil && !os.IsNotExist(binaryRemoveErr) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing eos binary: %v", binaryRemoveErr))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "removed binary")

	confirmed := flagYes || helpers.PromptConfirm(cmd, "remove eos system data? (y/n):")
	if confirmed {
		systemDataRemoveErr := os.RemoveAll(baseDir)
		if systemDataRemoveErr != nil && !os.IsNotExist(systemDataRemoveErr) {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing eos system data: %v", systemDataRemoveErr))
			helpers.PrintSudoHint(cmd)
			return helpers.ErrCommandFailed
		}
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "removed eos system data")
	} else {
		cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "skipped removal eos system data")
		cmd.Printf("%s %s\n\n", ui.TextMuted.Render("  to remove later, run:"), ui.TextMuted.Render(fmt.Sprintf("rm -rf %s", baseDir)))
	}

	// removeShellIntegration()

	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), "uninstall complete")
	return nil
}

func handleStoppingServices(cmd *cobra.Command, mgr manager.ServiceManager, cfg *config.SystemConfig, serviceInstances []types.ServiceInstance, flagYes bool) bool {
	confirmed := flagYes || helpers.PromptConfirm(cmd, "stop all services? (y/n):")

	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "uninstall canceled")
		return false
	}
	stoppedServices, erroredServices := stopServices(mgr, cfg, serviceInstances)

	numberStoppedServices := len(stoppedServices)
	if numberStoppedServices != 0 {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), fmt.Sprintf("stopped %d services", numberStoppedServices))
	}
	if len(erroredServices) != 0 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping services: %v", erroredServices))
		confirmed := helpers.PromptConfirm(cmd, "force stop remaining services? (y/n):")
		if !confirmed {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "uninstall canceled due to remaining active services")
			return false
		}
		err := forceStopServices(mgr, extractServiceInstancesFromErrors(erroredServices))
		if len(err) != 0 {
			cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warn"), fmt.Sprintf("force stopping services: %v", err))
		}
	}

	for _, serviceInstance := range serviceInstances {
		if _, removeErr := mgr.RemoveServiceInstance(serviceInstance.Name); removeErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleaning up service instance: %v", removeErr))
		}
	}

	return true
}

type StoppedServices map[string]manager.StopServiceResult

type ErrorResult struct {
	Error   error
	Service types.ServiceInstance
}
type ErrorServices map[string]ErrorResult

func stopServices(mgr manager.ServiceManager, cfg *config.SystemConfig, serviceInstances []types.ServiceInstance) (StoppedServices, ErrorServices) {
	stoppedServices := make(StoppedServices)
	erroredServices := make(ErrorServices)

	for _, serviceInstance := range serviceInstances {
		stopResult, err := mgr.StopService(serviceInstance.Name, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
		if err != nil {
			erroredServices[serviceInstance.Name] = ErrorResult{Service: serviceInstance, Error: err}
			continue
		}
		stoppedServices[serviceInstance.Name] = stopResult
	}

	return stoppedServices, erroredServices
}

func forceStopServices(mgr manager.ServiceManager, serviceInstances []types.ServiceInstance) ErrorServices {
	erroredServices := make(ErrorServices)

	for _, serviceInstance := range serviceInstances {
		_, err := mgr.ForceStopService(serviceInstance.Name)
		if err != nil {
			erroredServices[serviceInstance.Name] = ErrorResult{Service: serviceInstance, Error: err}
			continue
		}
	}

	return erroredServices
}

func extractServiceInstancesFromErrors(errorServices ErrorServices) []types.ServiceInstance {
	var serviceInstances []types.ServiceInstance
	for _, result := range errorServices {
		serviceInstances = append(serviceInstances, result.Service)
	}

	return serviceInstances
}
