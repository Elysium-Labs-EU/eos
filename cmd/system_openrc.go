package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
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
command_args="daemon start --foreground"
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
//
// openrcStartupParams bundles the plain-value parameters it needs to
// render, write, and enable an OpenRC init script.
type openrcStartupParams struct {
	InstallDir   string
	DaemonConfig *config.StandaloneDaemonConfig
	InitDir      string
	InitFile     string
	Verbose      bool
	FlagYes      bool
}

func openrcStartupCmd(ctx context.Context, cmd *cobra.Command, p openrcStartupParams, detectRuntime func() (string, error), run runCmdFn) error {
	if err := ensureRuntime(cmd, p.Verbose, detectRuntime, "openrc"); err != nil {
		return err
	}

	fullTargetName := filepath.Join(p.InitDir, p.InitFile)
	helpers.Debugf(cmd, p.Verbose, "target init script: %s", fullTargetName)

	if err := checkWritable(cmd, p.InitDir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}

	effectiveUser, effectiveUserErr := userutil.EffectiveUser()
	if effectiveUserErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting current user: %v", effectiveUserErr))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, p.Verbose, "effective user: %s", effectiveUser.Username)

	script, err := renderOpenRCScript(p.InstallDir, effectiveUser.Username)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("rendering init script: %v", err))
		return helpers.ErrCommandFailed
	}

	if !confirmOrDecline(cmd, p.FlagYes, "create OpenRC init script? (y/n):", "init script creation canceled") {
		return nil
	}

	if err = os.WriteFile(fullTargetName, []byte(script), 0755); err != nil { // #nosec G306 -- OpenRC requires init scripts to be executable
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing init script: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("init script created, at:"), fullTargetName)

	unit := p.InitFile
	helpers.Debugf(cmd, p.Verbose, "running: rc-update add %s default", unit)
	out, err := run(ctx, "rc-update", "add", unit, "default")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("enabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, p.Verbose, "rc-update output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service enabled, eos will start on boot")

	if !confirmOrDecline(cmd, p.FlagYes, "restart daemon now? (y/n):", "daemon will be managed by OpenRC on next start") {
		return nil
	}

	if stopErr := stopStandaloneForRestart(cmd, p.DaemonConfig); stopErr != nil {
		return stopErr
	}

	helpers.Debugf(cmd, p.Verbose, "running: rc-service %s start", unit)
	out, err = run(ctx, "rc-service", unit, "start")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, p.Verbose, "rc-service output: %s", strings.TrimSpace(string(out)))
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}

// openrcUnstartupCmd is the OpenRC counterpart to unstartupCmd.
// openrcUnstartupParams bundles the plain-value parameters it needs to
// disable and remove an OpenRC init script.
type openrcUnstartupParams struct {
	InitDir  string
	InitFile string
	Verbose  bool
	FlagYes  bool
}

func openrcUnstartupCmd(ctx context.Context, cmd *cobra.Command, p openrcUnstartupParams, detectRuntime func() (string, error), run runCmdFn, identity userutil.Identity) error {
	if err := ensureRuntime(cmd, p.Verbose, detectRuntime, "openrc"); err != nil {
		return err
	}

	if !confirmOrDecline(cmd, p.FlagYes, "remove OpenRC init script and disable eos on boot? (y/n):", "canceled") {
		return nil
	}

	unit := p.InitFile
	helpers.Debugf(cmd, p.Verbose, "running: rc-service %s stop", unit)
	out, err := run(ctx, "rc-service", unit, "stop")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service stopped")

	helpers.Debugf(cmd, p.Verbose, "running: rc-update del %s default", unit)
	out, err = run(ctx, "rc-update", "del", unit, "default")
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("disabling service: %v", string(out)))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "service disabled")

	fullTargetName := filepath.Join(p.InitDir, p.InitFile)
	if err = os.Remove(fullTargetName); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing init script: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), "init script removed, startup disabled")

	if !confirmOrDecline(cmd, p.FlagYes, "restart daemon standalone? (y/n):", "") {
		return nil
	}

	if err := forkDaemon(ctx, &config.StandaloneDaemonConfig{PIDFile: config.DaemonPIDFile, SocketPath: config.DaemonSocketPath}, false, identity); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos daemon logs") + ui.TextMuted.Render(" → check daemon logs") + "\n")
		return helpers.ErrCommandFailed
	}
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "daemon started in background")
	return nil
}
