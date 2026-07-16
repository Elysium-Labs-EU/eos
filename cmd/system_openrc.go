package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/process"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// openRCScriptTemplate uses supervise-daemon, OpenRC's supervisor for
// long-running processes (backgrounding, pidfile, respawn) — the OpenRC
// analog of systemd's Type=simple + Restart=always.
const openRCScriptTemplate = `#!/sbin/openrc-run

name="eos"
description="eos deployment daemon"
supervisor="supervise-daemon"
command="{{.ExecStart}}"
command_args="daemon start"
command_user="{{.User}}"
pidfile="/run/${RC_SVCNAME}.pid"
respawn_delay=5

depend() {
	need net
}
`

type openRCData struct {
	ExecStart string
	User      string
}

func renderOpenRCScript(installDir, user string) (string, error) {
	tmpl, err := template.New("openrc").Parse(openRCScriptTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	data := openRCData{
		ExecStart: filepath.Join(installDir, "eos"),
		User:      user,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	return buf.String(), nil
}

// openrcStartupCmd is the OpenRC counterpart to startupCmd. OpenRC has no
// per-user service manager equivalent to `systemctl --user`, so this always
// installs a system-wide script and requires root.
func openrcStartupCmd(ctx context.Context, cmd *cobra.Command, installDir string, daemonConfig *config.StandaloneDaemonConfig, initDir, initFile string, verbose bool, detectRuntime func() (string, error), run runCmdFn) error { //nolint:unparam // initFile drives the rc-update/rc-service unit name; varies in tests so calls target a throwaway unit instead of the real one
	runtime, err := detectRuntime()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "detected runtime: %s", runtime)
	if runtime != "openrc" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("managing startup file not supported for this runtime: %v", runtime))
		return helpers.ErrCommandFailed
	}

	fullTargetName := filepath.Join(initDir, initFile)
	helpers.Debugf(cmd, verbose, "target init script: %s", fullTargetName)

	if err = checkWritable(cmd, initDir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}

	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "effective user: %s", effectiveUser.Username)

	script, err := renderOpenRCScript(installDir, effectiveUser.Username)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("rendering init script: %v", err))
		return helpers.ErrCommandFailed
	}

	confirmed := helpers.PromptConfirm(cmd, "create OpenRC init script? (y/n):")
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "init script creation canceled")
		return nil
	}

	if err = os.WriteFile(fullTargetName, []byte(script), 0755); err != nil { // #nosec G306 -- OpenRC requires init scripts to be executable
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing init script: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("init script created, at:"), fullTargetName)

	unit := initFile
	helpers.Debugf(cmd, verbose, "running: rc-update add %s default", unit)
	out, err := run(ctx, "rc-update", "add", unit, "default")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "rc-update output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service enabled, eos will start on boot")

	confirmed = helpers.PromptConfirm(cmd, "restart daemon now? (y/n):")
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon will be managed by OpenRC on next start")
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

	helpers.Debugf(cmd, verbose, "running: rc-service %s start", unit)
	out, err = run(ctx, "rc-service", unit, "start")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "rc-service output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

// openrcUnstartupCmd is the OpenRC counterpart to unstartupCmd.
func openrcUnstartupCmd(ctx context.Context, cmd *cobra.Command, initDir, initFile string, verbose bool, detectRuntime func() (string, error), run runCmdFn) error { //nolint:unparam // initFile drives the rc-update/rc-service unit name; varies in tests so calls target a throwaway unit instead of the real one
	runtime, err := detectRuntime()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "detected runtime: %s", runtime)
	if runtime != "openrc" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("managing startup file not supported for this runtime: %v", runtime))
		return helpers.ErrCommandFailed
	}

	confirmed := helpers.PromptConfirm(cmd, "remove OpenRC init script and disable eos on boot? (y/n):")
	if !confirmed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "canceled")
		return nil
	}

	unit := initFile
	helpers.Debugf(cmd, verbose, "running: rc-service %s stop", unit)
	out, err := run(ctx, "rc-service", unit, "stop")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service stopped")

	helpers.Debugf(cmd, verbose, "running: rc-update del %s default", unit)
	out, err = run(ctx, "rc-update", "del", unit, "default")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("disabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service disabled")

	fullTargetName := filepath.Join(initDir, initFile)
	if err = os.Remove(fullTargetName); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing init script: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), "init script removed, startup disabled")

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
