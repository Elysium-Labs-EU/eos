package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell_ReturnsBasename(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	if got := detectShell(); got != "zsh" {
		t.Errorf("got %q, want %q", got, "zsh")
	}
}

func TestDetectShell_EmptyEnv(t *testing.T) {
	t.Setenv("SHELL", "")
	if got := detectShell(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestDetectShell_Bash(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/bash")
	if got := detectShell(); got != "bash" {
		t.Errorf("got %q, want %q", got, "bash")
	}
}

func TestCompletionTargetPath_Zsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := completionTargetPath("zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".zsh", "completions", "_eos")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompletionTargetPath_Bash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := completionTargetPath("bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "eos")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompletionTargetPath_Fish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := completionTargetPath("fish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".config", "fish", "completions", "eos.fish")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompletionTargetPath_Unsupported(t *testing.T) {
	_, err := completionTargetPath("tcsh")
	if err == nil {
		t.Error("expected error for unsupported shell")
	}
}

func TestWriteCompletionScript_Zsh(t *testing.T) {
	root, _, _, _ := setupCmd(t)
	path := filepath.Join(t.TempDir(), "completions", "_eos")

	if err := writeCompletionScript(root, "zsh", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if !strings.Contains(string(data), "#compdef") {
		t.Errorf("zsh script missing #compdef header")
	}
}

func TestWriteCompletionScript_Bash(t *testing.T) {
	root, _, _, _ := setupCmd(t)
	path := filepath.Join(t.TempDir(), "completions", "eos")

	if err := writeCompletionScript(root, "bash", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if len(data) == 0 {
		t.Error("bash completion script is empty")
	}
}

func TestWriteCompletionScript_Fish(t *testing.T) {
	root, _, _, _ := setupCmd(t)
	path := filepath.Join(t.TempDir(), "completions", "eos.fish")

	if err := writeCompletionScript(root, "fish", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if !strings.Contains(string(data), "complete") {
		t.Errorf("fish script missing 'complete'")
	}
}

func TestWriteCompletionScript_CreatesParentDir(t *testing.T) {
	root, _, _, _ := setupCmd(t)
	path := filepath.Join(t.TempDir(), "deep", "nested", "dir", "_eos")

	if err := writeCompletionScript(root, "zsh", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWriteCompletionScript_UnsupportedShell(t *testing.T) {
	root, _, _, _ := setupCmd(t)
	path := filepath.Join(t.TempDir(), "eos")

	if err := writeCompletionScript(root, "tcsh", path); err == nil {
		t.Error("expected error for unsupported shell")
	}
}

func TestCompletionZshCmd_PrintsToStdout(t *testing.T) {
	root, out, _, _ := setupCmd(t)
	root.SetArgs([]string{"completion", "zsh"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "#compdef") {
		t.Errorf("expected #compdef in stdout, got: %s", out.String())
	}
}

func TestCompletionBashCmd_PrintsToStdout(t *testing.T) {
	root, out, _, _ := setupCmd(t)
	root.SetArgs([]string{"completion", "bash"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() == 0 {
		t.Error("expected bash completion script on stdout, got nothing")
	}
}

func TestCompletionFishCmd_PrintsToStdout(t *testing.T) {
	root, out, _, _ := setupCmd(t)
	root.SetArgs([]string{"completion", "fish"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "complete") {
		t.Errorf("expected 'complete' in fish output, got: %s", out.String())
	}
}

func TestCompletionInteractive_NoShell(t *testing.T) {
	t.Setenv("SHELL", "")
	root, _, errBuf, _ := setupCmd(t)
	root.SetArgs([]string{"completion"})
	root.SetIn(&slowReader{strings.NewReader("")})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errBuf.String(), "could not detect shell") {
		t.Errorf("expected shell detection hint in stderr, got: %s", errBuf.String())
	}
}

func TestCompletionInteractive_UnsupportedShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/tcsh")
	root, _, errBuf, _ := setupCmd(t)
	root.SetArgs([]string{"completion"})
	root.SetIn(&slowReader{strings.NewReader("")})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected unsupported shell message, got: %s", errBuf.String())
	}
}

func TestCompletionInteractive_Decline(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, out, _, _ := setupCmd(t)
	root.SetArgs([]string{"completion"})
	root.SetIn(&slowReader{strings.NewReader("n\n")})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	targetPath := filepath.Join(home, ".zsh", "completions", "_eos")
	if _, err := os.Stat(targetPath); err == nil {
		t.Error("completion file should not be written on decline")
	}
	if !strings.Contains(out.String(), "Skipped") {
		t.Errorf("expected 'Skipped' in output, got: %s", out.String())
	}
}

func TestCompletionInteractive_Confirm_Zsh(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, out, _, _ := setupCmd(t)
	root.SetArgs([]string{"completion"})
	root.SetIn(&slowReader{strings.NewReader("y\n")})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	targetPath := filepath.Join(home, ".zsh", "completions", "_eos")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("completion file not written: %v", err)
	}
	if !strings.Contains(string(data), "#compdef") {
		t.Errorf("written file missing #compdef header")
	}

	outStr := out.String()
	if !strings.Contains(outStr, "installed") {
		t.Errorf("expected 'installed' in output, got: %s", outStr)
	}

	zshrc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("~/.zshrc not written: %v", err)
	}
	if !strings.Contains(string(zshrc), ".zsh/completions") {
		t.Errorf("~/.zshrc missing fpath entry, got: %s", string(zshrc))
	}
	if !strings.Contains(string(zshrc), "compinit") {
		t.Errorf("~/.zshrc missing compinit, got: %s", string(zshrc))
	}
	if !strings.Contains(outStr, "patched") {
		t.Errorf("expected 'patched' in output, got: %s", outStr)
	}
}

func TestPatchZshrc_WritesWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	patched, err := patchZshrc("/home/user/.zsh/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patched {
		t.Error("expected patched=true for new .zshrc")
	}

	data, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf(".zshrc not written: %v", err)
	}
	if !strings.Contains(string(data), "fpath=(/home/user/.zsh/completions $fpath)") {
		t.Errorf("fpath line missing, got: %s", string(data))
	}
	if !strings.Contains(string(data), "compinit") {
		t.Errorf("compinit missing, got: %s", string(data))
	}
}

func TestPatchZshrc_SkipsIfAlreadyPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	zshrc := filepath.Join(home, ".zshrc")
	existing := "fpath=(/home/user/.zsh/completions $fpath)\nautoload -Uz compinit && compinit\n"
	if err := os.WriteFile(zshrc, []byte(existing), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	patched, err := patchZshrc("/home/user/.zsh/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patched {
		t.Error("expected patched=false when entry already present")
	}

	data, _ := os.ReadFile(zshrc)
	if string(data) != existing {
		t.Errorf("file was modified when it should not have been")
	}
}

func TestPatchZshrc_AppendsToExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("export PATH=$PATH:/usr/local/bin\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	patched, err := patchZshrc("/home/user/.zsh/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patched {
		t.Error("expected patched=true")
	}

	data, _ := os.ReadFile(zshrc)
	content := string(data)
	if !strings.Contains(content, "export PATH") {
		t.Error("existing content was lost")
	}
	if !strings.Contains(content, "fpath=(/home/user/.zsh/completions $fpath)") {
		t.Errorf("fpath line not appended, got: %s", content)
	}
}
