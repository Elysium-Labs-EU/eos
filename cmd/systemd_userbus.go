package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

type runCmdFn func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- name is always "systemctl"
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

// dirAccessCheckFn matches isAccessibleDir's signature; ensureUserBusAvailable takes it as a
// parameter (isAccessibleDir in production) instead of calling isAccessibleDir directly, so tests
// can inject a fake that doesn't depend on the real /run/user/<uid> — needed because on a root-run
// CI job /run/user/0 genuinely exists and is root-owned, making the real check pass in an
// environment a test wants to treat as "no bus available".
type dirAccessCheckFn func(path string, uid int) bool

// correctUserRuntimeDir silently corrects XDG_RUNTIME_DIR to expected when the current value
// isn't valid for uid but expected is — the same non-interactive auto-heal ensureUserBusAvailable
// performs before its interactive (prompt-for-linger) fallback, but with no *cobra.Command to print
// through: for a caller like systemdMainPID, whose own caller already silently skips on an
// unresolved bus rather than surfacing a failure, an interactive prompt would be inappropriate
// anyway. Returns true once the env var is confirmed correct (already, or after correction); false
// when neither the current nor expected dir is usable, in which case the caller is left to its own
// existing (silent-skip) failure handling rather than this function ever prompting.
func correctUserRuntimeDir(uid int, expected string, checkDir dirAccessCheckFn) (bool, error) {
	if checkDir(os.Getenv("XDG_RUNTIME_DIR"), uid) {
		return true, nil
	}
	if !checkDir(expected, uid) {
		return false, nil
	}
	if err := os.Setenv("XDG_RUNTIME_DIR", expected); err != nil {
		return false, fmt.Errorf("setting XDG_RUNTIME_DIR: %w", err)
	}
	return true, nil
}

// ensureUserBusAvailable diagnoses and, where possible, auto-fixes the "no systemd user bus"
// condition that causes `systemctl --user ...` to fail with "Failed to connect to bus: Permission
// denied". This happens when XDG_RUNTIME_DIR is unset/stale (fixable by correcting the env var) or
// when the account has no active session and no linger enabled (fixable via `loginctl enable-linger`).
// expected is the runtime dir this process should be using (userRuntimeDir(uid) in production;
// injected directly in tests so they don't depend on the real /run/user/<uid>). uid is the target
// user's uid — the user the systemd --user session belongs to, resolved via
// userutil.EffectiveUser() by the caller, not necessarily os.Getuid() (root under sudo). checkDir
// is isAccessibleDir in production; tests inject a fake to stay deterministic regardless of what
// runtime dirs genuinely exist in the environment running the test.
func ensureUserBusAvailable(ctx context.Context, cmd *cobra.Command, verbose bool, username string, uid int, expected string, run runCmdFn, checkDir dirAccessCheckFn) error {
	current := os.Getenv("XDG_RUNTIME_DIR")
	helpers.Debugf(cmd, verbose, "XDG_RUNTIME_DIR=%q (expected %q)", current, expected)

	if checkDir(current, uid) {
		return nil
	}

	if checkDir(expected, uid) {
		helpers.Debugf(cmd, verbose, "correcting XDG_RUNTIME_DIR to %q", expected)
		if err := os.Setenv("XDG_RUNTIME_DIR", expected); err != nil {
			return fmt.Errorf("setting XDG_RUNTIME_DIR: %w", err)
		}
		return nil
	}

	cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), "no active systemd user session found — user bus is not running")
	cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), "this happens when the account has no login session and linger is not enabled")

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("enable linger for %q to start a user bus now? (y/n):", username))
	if !confirmed {
		return fmt.Errorf("no user bus available and linger was not enabled")
	}

	out, err := run(ctx, "loginctl", "enable-linger", username)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("enable-linger: %v", string(out)))
		helpers.PrintSudoHint(cmd)
		cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), fmt.Sprintf("run manually: %s", ui.TextCommand.Render("sudo loginctl enable-linger "+username)))
		return fmt.Errorf("enabling linger: %w", err)
	}
	helpers.Debugf(cmd, verbose, "loginctl enable-linger %s succeeded", username)

	for attempt := 1; attempt <= 5; attempt++ {
		helpers.Debugf(cmd, verbose, "checking for %q (attempt %d/5)", expected, attempt)
		if checkDir(expected, uid) {
			if err := os.Setenv("XDG_RUNTIME_DIR", expected); err != nil {
				return fmt.Errorf("setting XDG_RUNTIME_DIR: %w", err)
			}
			cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "user bus is now available")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("user bus still unavailable after enabling linger — a fresh login may be required")
}
