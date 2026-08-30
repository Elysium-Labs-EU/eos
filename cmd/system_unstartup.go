package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// sysRunUnstartup backs the "system unstartup" subcommand's RunE: it
// resolves shared config/flags, then dispatches to the darwin or
// systemd/OpenRC path.
func sysRunUnstartup(cmd *cobra.Command, systemCmd *cobra.Command) error {
	_, systemConfig, identity, verbose, flagYes, err := loadSystemConfigAndFlags(cmd, systemCmd)
	if err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		return sysUnstartupDarwin(cmd, systemCmd, systemConfig.Daemon, verbose, flagYes, identity)
	}
	return sysUnstartupLinux(cmd, systemCmd, systemConfig.Daemon, verbose, flagYes, identity)
}

// sysUnstartupDarwin handles "system unstartup" on macOS: remove the
// installed launchd plist, if any is configured.
func sysUnstartupDarwin(cmd *cobra.Command, systemCmd *cobra.Command, daemon config.DaemonConfig, verbose, flagYes bool, identity userutil.Identity) error {
	if daemon.Launchd == nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "no launchd startup configured for this user — nothing to remove")
		return helpers.ErrCommandFailed
	}
	userAgent := os.Getuid() != 0
	return unstartupCmdLaunchd(cmd.Context(), cmd, launchdUnstartupParams{
		DaemonConfig: *daemon.Launchd,
		UserAgent:    userAgent,
		Verbose:      verbose,
		FlagYes:      flagYes,
	}, execRunCmd, identity)
}

// sysUnstartupLinux handles "system unstartup" on non-macOS platforms:
// detect the active init system and remove the OpenRC init script or the
// configured systemd unit.
func sysUnstartupLinux(cmd *cobra.Command, systemCmd *cobra.Command, daemon config.DaemonConfig, verbose, flagYes bool, identity userutil.Identity) error {
	runtimeName, err := detectActiveSystemRuntime()
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}

	if runtimeName == "openrc" {
		return openrcUnstartupCmd(cmd.Context(), cmd, openrcUnstartupParams{
			InitDir:  config.OpenRCInitDir,
			InitFile: config.OpenRCTargetFileName,
			Verbose:  verbose,
			FlagYes:  flagYes,
		}, detectActiveSystemRuntime, execRunCmd, identity)
	}

	if daemon.Systemd == nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "no systemd startup configured for this user — nothing to remove")
		return helpers.ErrCommandFailed
	}
	userUnit := os.Getuid() != 0
	return unstartupCmd(cmd.Context(), cmd, systemdUnstartupParams{
		DaemonConfig: *daemon.Systemd,
		UserUnit:     userUnit,
		Verbose:      verbose,
		FlagYes:      flagYes,
	}, detectActiveSystemRuntime, execRunCmd, identity)
}

// systemdUnstartupParams bundles the plain-value parameters unstartupCmd
// needs to disable and remove a systemd unit.
type systemdUnstartupParams struct {
	DaemonConfig config.SystemdConfig
	UserUnit     bool
	Verbose      bool
	FlagYes      bool
}

func unstartupCmd(ctx context.Context, cmd *cobra.Command, p systemdUnstartupParams, detectRuntime func() (string, error), run runCmdFn, identity userutil.Identity) error {
	if err := ensureRuntime(cmd, p.Verbose, detectRuntime, "systemd"); err != nil {
		return err
	}

	unitKind := unitScope(p.UserUnit)

	if !confirmOrDecline(cmd, p.FlagYes, fmt.Sprintf("remove %s and disable eos on boot? (y/n):", unitKind), "canceled") {
		return nil
	}

	if err := prepareUserBusIfNeeded(ctx, cmd, p.Verbose, p.UserUnit, run); err != nil {
		return err
	}

	unit := unitName(p.DaemonConfig.SystemdTargetFileName)
	unitPath := p.DaemonConfig.SystemdTargetDir + p.DaemonConfig.SystemdTargetFileName
	if err := disableAndRemoveSystemdUnit(ctx, cmd, systemdUnitRemoval{
		Verbose:  p.Verbose,
		UserUnit: p.UserUnit,
		UnitKind: unitKind,
		Unit:     unit,
		UnitPath: unitPath,
	}, run); err != nil {
		return err
	}

	if p.UserUnit {
		cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), "if you enabled linger, also run: loginctl disable-linger <username>")
	}

	return restartDaemonStandaloneIfConfirmed(ctx, cmd, p.FlagYes, identity)
}

// prepareUserBusIfNeeded resolves the invoking user and prepares their
// systemd user bus, but only when userUnit is set — a no-op for system units.
func prepareUserBusIfNeeded(ctx context.Context, cmd *cobra.Command, verbose, userUnit bool, run runCmdFn) error {
	if !userUnit {
		return nil
	}
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return helpers.ErrCommandFailed
	}
	return prepareUserBus(ctx, cmd, verbose, effectiveUser, run)
}

// disableAndRemoveSystemdUnit stops and disables the unit, removes its file, and
// reloads systemd. Any step failure prints context and returns ErrCommandFailed.
// systemdUnitRemoval bundles the unit identity and flags disableAndRemoveSystemdUnit
// needs to stop, disable, and delete a systemd unit.
type systemdUnitRemoval struct {
	UnitKind string
	Unit     string
	UnitPath string
	Verbose  bool
	UserUnit bool
}

func disableAndRemoveSystemdUnit(ctx context.Context, cmd *cobra.Command, u systemdUnitRemoval, run runCmdFn) error {
	helpers.Debugf(cmd, u.Verbose, "running: systemctl %s", strings.Join(systemctlArgs(u.UserUnit, "stop", u.Unit), " "))
	out, err := run(ctx, "systemctl", systemctlArgs(u.UserUnit, "stop", u.Unit)...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsgLn, ui.LabelError.Render("error"), fmt.Sprintf("stopping %s: %v", u.UnitKind, string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), u.UnitKind+" stopped")

	helpers.Debugf(cmd, u.Verbose, "running: systemctl %s", strings.Join(systemctlArgs(u.UserUnit, "disable", u.Unit), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(u.UserUnit, "disable", u.Unit)...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsgLn, ui.LabelError.Render("error"), fmt.Sprintf("disabling %s: %v", u.UnitKind, string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), u.UnitKind+" disabled")

	if err = os.Remove(u.UnitPath); err != nil {
		cmd.PrintErrf(fmtLabelMsgLn, ui.LabelError.Render("error"), fmt.Sprintf("removing unit file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "unit file removed")

	helpers.Debugf(cmd, u.Verbose, "running: systemctl %s", strings.Join(systemctlArgs(u.UserUnit, "daemon-reload"), " "))
	out, err = run(ctx, "systemctl", systemctlArgs(u.UserUnit, "daemon-reload")...)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsgLn, ui.LabelError.Render("error"), fmt.Sprintf("daemon-reload: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelSuccess.Render("success"), u.UnitKind+" startup removed")
	return nil
}

// unstartupCmdLaunchd is the launchd (macOS) analog of unstartupCmd. "launchctl
// bootout" both stops the job and unloads it in one step (the combined equivalent of
// "systemctl stop" + "systemctl disable"): the plist stays on disk until removed below,
// but won't be re-bootstrapped until the next "eos system startup", boot, or login.
// launchdUnstartupParams bundles the plain-value parameters
// unstartupCmdLaunchd needs to boot out and remove a launchd plist.
type launchdUnstartupParams struct {
	DaemonConfig config.LaunchdConfig
	UserAgent    bool
	Verbose      bool
	FlagYes      bool
}

func unstartupCmdLaunchd(ctx context.Context, cmd *cobra.Command, p launchdUnstartupParams, run runCmdFn, identity userutil.Identity) error {
	scopeKind := launchdScope(p.UserAgent)

	if !confirmOrDecline(cmd, p.FlagYes, fmt.Sprintf("remove %s and disable eos on boot? (y/n):", scopeKind), "canceled") {
		return nil
	}

	uid, err := resolveUnstartupLaunchdUID(cmd, p.UserAgent)
	if err != nil {
		return err
	}
	domain := launchdDomain(p.UserAgent, uid)
	label := launchdLabel(p.DaemonConfig.LaunchdPlistFileName)
	target := domain + "/" + label

	if err := launchdBootout(ctx, cmd, p.Verbose, scopeKind, target, run); err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(p.DaemonConfig.LaunchdTargetDir, p.DaemonConfig.LaunchdPlistFileName)); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("removing plist file: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsg, ui.LabelSuccess.Render("success"), scopeKind+" startup removed")

	return restartDaemonStandaloneIfConfirmed(ctx, cmd, p.FlagYes, identity)
}

// launchdBootout stops and unloads the launchd job, treating "not loaded"
// (exit code 3 — "No such process") as already-stopped rather than fatal:
// unlike "systemctl stop" (idempotent, exits 0 on an already-stopped unit),
// "launchctl bootout" exits 3 when the job isn't currently loaded — verified
// empirically. Without this, "eos system unstartup" would hard-fail and never
// remove the plist whenever the job happened to already be stopped.
func launchdBootout(ctx context.Context, cmd *cobra.Command, verbose bool, scopeKind, target string, run runCmdFn) error {
	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	out, err := run(ctx, "launchctl", "bootout", target)
	if err == nil {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), scopeKind+" stopped and unloaded")
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		helpers.Debugf(cmd, verbose, "launchctl bootout: job was not loaded")
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(scopeKind+" was not loaded"))
		return nil
	}
	cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping %s: %v", scopeKind, string(out)))
	return helpers.ErrCommandFailed
}

// resolveUnstartupLaunchdUID resolves the invoking user (only when userAgent
// is set) and returns the uid used to build the launchctl target domain.
func resolveUnstartupLaunchdUID(cmd *cobra.Command, userAgent bool) (int, error) {
	if !userAgent {
		return os.Getuid(), nil
	}
	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return 0, helpers.ErrCommandFailed
	}
	return resolveLaunchdUID(cmd, userAgent, effectiveUser)
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
