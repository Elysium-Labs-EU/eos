package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Set up shell tab completion",
		Long: `Set up tab completion so that eos commands and service names complete on <Tab>.

Running without a subcommand detects your shell and prompts to install.
To print the script to stdout instead (for manual setup or scripting), pass the shell name:

  eos completion bash
  eos completion zsh
  eos completion fish`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractiveCompletion(cmd, root)
		},
	}

	cmd.AddCommand(newCompletionBashCmd(root))
	cmd.AddCommand(newCompletionZshCmd(root))
	cmd.AddCommand(newCompletionFishCmd(root))

	return cmd
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	return filepath.Base(shell)
}

func completionTargetPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "eos"), nil
	case "zsh":
		return filepath.Join(home, ".zsh", "completions", "_eos"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "eos.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

func writeCompletionScript(root *cobra.Command, shell, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path derived from completionTargetPath
	if err != nil {
		return err
	}
	var genErr error
	switch shell {
	case "bash":
		genErr = root.GenBashCompletionV2(f, true)
	case "zsh":
		genErr = root.GenZshCompletion(f)
	case "fish":
		genErr = root.GenFishCompletion(f, true)
	default:
		genErr = fmt.Errorf("unsupported shell: %s", shell)
	}
	if closeErr := f.Close(); closeErr != nil && genErr == nil {
		return closeErr
	}
	return genErr
}

// refreshInstalledCompletions regenerates completion scripts for shells that already have one
// installed, after a version upgrade replaces the eos binary on disk. It shells out to the new
// binary (rather than using the in-process root command) because the running process still holds
// the old CLI surface in memory; only the new binary knows about commands/flags it just added.
func refreshInstalledCompletions(ctx context.Context, cmd *cobra.Command, binaryPath string) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		targetPath, err := completionTargetPath(shell)
		if err != nil {
			continue
		}
		if _, statErr := os.Stat(targetPath); statErr != nil {
			continue // not installed for this shell; nothing to refresh
		}

		out, err := exec.CommandContext(ctx, binaryPath, "completion", shell).Output() // #nosec G204 -- binaryPath is the eos binary just installed by system update
		if err != nil {
			cmd.Printf("%s %s\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("could not refresh %s completion: %v", shell, err))
			continue
		}
		if writeErr := os.WriteFile(targetPath, out, 0o600); writeErr != nil {
			cmd.Printf("%s %s\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("could not write refreshed %s completion: %v", shell, writeErr))
			continue
		}
		cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), fmt.Sprintf("refreshed %s completion", shell))
	}
}

func runInteractiveCompletion(cmd *cobra.Command, root *cobra.Command) error {
	shell := detectShell()
	if shell == "" {
		cmd.PrintErrln("  could not detect shell; run 'eos completion bash|zsh|fish' to print the script manually")
		return nil
	}
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		cmd.PrintErrf("  shell %q not supported; run 'eos completion bash|zsh|fish' to print the script manually\n", shell)
		return nil
	}

	targetPath, err := completionTargetPath(shell)
	if err != nil {
		return err
	}

	cmd.Printf("\n  %s %s\n\n", ui.TextMuted.Render("Detected shell:"), ui.TextBold.Render(shell))

	confirmed := helpers.PromptConfirm(cmd, fmt.Sprintf("Install tab completion for %s? [y/N]", shell))
	if !confirmed {
		cmd.Printf("\n  %s\n\n", ui.TextMuted.Render("Skipped. Run 'eos completion "+shell+"' to print the script manually."))
		return nil
	}

	if err := writeCompletionScript(root, shell, targetPath); err != nil {
		return fmt.Errorf("writing completion script: %w", err)
	}

	cmd.Printf("\n  %s %s\n", ui.LabelSuccess.Render("installed →"), ui.TextCommand.Render(targetPath))

	if shell == "zsh" {
		if patched, patchErr := patchZshrc(filepath.Dir(targetPath)); patchErr != nil {
			cmd.Printf("  %s %s\n", ui.LabelError.Render("could not patch ~/.zshrc:"), patchErr.Error())
		} else if patched {
			cmd.Printf("  %s %s\n", ui.LabelSuccess.Render("patched →"), ui.TextCommand.Render("~/.zshrc"))
		} else {
			cmd.Printf("  %s\n", ui.TextMuted.Render("~/.zshrc already has fpath entry — no change"))
		}
	}
	cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("reload shell:"), ui.TextCommand.Render("exec $SHELL"))

	return nil
}

func patchZshrc(completionDir string) (patched bool, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	zshrc := filepath.Join(home, ".zshrc")

	existing, err := os.ReadFile(zshrc) //nolint:gosec // path derived from os.UserHomeDir
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	fpathLine := fmt.Sprintf("fpath=(%s $fpath)", completionDir)
	if strings.Contains(string(existing), completionDir) {
		return false, nil
	}

	f, err := os.OpenFile(zshrc, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // user home dir
	if err != nil {
		return false, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	prefix := "\n"
	if len(existing) == 0 {
		prefix = ""
	}
	_, err = fmt.Fprintf(f, "%s# eos tab completion\n%s\nautoload -Uz compinit && compinit\n", prefix, fpathLine)
	if err != nil {
		return false, err
	}
	return true, nil
}

func newCompletionBashCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "bash",
		Short: "Print bash completion script to stdout",
		Long: `Print the bash completion script to stdout.

Install system-wide (requires sudo):
  sudo eos completion bash > /etc/bash_completion.d/eos

Install for current user (no sudo):
  eos completion bash > ~/.local/share/bash-completion/completions/eos`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenBashCompletionV2(cmd.OutOrStdout(), true)
		},
	}
}

func newCompletionZshCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "zsh",
		Short: "Print zsh completion script to stdout",
		Long: `Print the zsh completion script to stdout.

Install:
  eos completion zsh > "${fpath[1]}/_eos"

Then reload: exec $SHELL`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenZshCompletion(cmd.OutOrStdout())
		},
	}
}

func newCompletionFishCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "fish",
		Short: "Print fish completion script to stdout",
		Long: `Print the fish completion script to stdout.

Install:
  eos completion fish > ~/.config/fish/completions/eos.fish`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return root.GenFishCompletion(cmd.OutOrStdout(), true)
		},
	}
}
