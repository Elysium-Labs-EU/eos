package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

// --- UserSystemdDir / UserLaunchAgentsDir ---

func TestUserSystemdDir(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		got, err := UserSystemdDir()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(home, ".config", "systemd", "user") + "/"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("home dir unresolvable", func(t *testing.T) {
		t.Setenv("HOME", "")
		if _, err := UserSystemdDir(); err == nil {
			t.Fatal("expected error when HOME is unset, got nil")
		}
	})
}

func TestUserLaunchAgentsDir(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		got, err := UserLaunchAgentsDir()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(home, "Library", "LaunchAgents") + "/"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("home dir unresolvable", func(t *testing.T) {
		t.Setenv("HOME", "")
		if _, err := UserLaunchAgentsDir(); err == nil {
			t.Fatal("expected error when HOME is unset, got nil")
		}
	})
}

// --- GetInstallDir ---

func TestGetInstallDir(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("EOS_INSTALL_DIR", "")
		if got := GetInstallDir(); got != InstallDir {
			t.Errorf("got %q, want default %q", got, InstallDir)
		}
	})

	t.Run("override", func(t *testing.T) {
		t.Setenv("EOS_INSTALL_DIR", "/opt/eos/bin")
		if got := GetInstallDir(); got != "/opt/eos/bin" {
			t.Errorf("got %q, want override", got)
		}
	})
}

// --- IsUnderSystemd / IsUnderLaunchd ---

func TestIsUnderSystemd(t *testing.T) {
	t.Run("not under systemd", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "")
		if IsUnderSystemd() {
			t.Error("expected false when INVOCATION_ID is unset")
		}
	})

	t.Run("under systemd", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "abc123")
		if !IsUnderSystemd() {
			t.Error("expected true when INVOCATION_ID is set")
		}
	})
}

func TestIsUnderLaunchd(t *testing.T) {
	t.Run("not under launchd", func(t *testing.T) {
		t.Setenv("XPC_SERVICE_NAME", "")
		if IsUnderLaunchd() {
			t.Error("expected false when XPC_SERVICE_NAME is unset")
		}
	})

	t.Run("under launchd", func(t *testing.T) {
		t.Setenv("XPC_SERVICE_NAME", "org.elysiumlabs.eos")
		if !IsUnderLaunchd() {
			t.Error("expected true when XPC_SERVICE_NAME is set")
		}
	})
}

// --- IsSystemdManaged / IsLaunchdManaged ---
//
// Both share the same os.Stat-based shape, so they're driven by the same
// table with the function under test selected per case.

func TestIsSystemdManaged(t *testing.T) {
	const unitFile = "eos.service"

	t.Run("not installed", func(t *testing.T) {
		managed, err := IsSystemdManaged(t.TempDir(), unitFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected not managed when unit file is absent")
		}
	})

	t.Run("installed", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, unitFile), []byte("[Unit]\n"), 0644); err != nil {
			t.Fatalf("writing unit file: %v", err)
		}
		managed, err := IsSystemdManaged(dir, unitFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !managed {
			t.Error("expected managed when unit file exists")
		}
	})

	t.Run("dangling symlink treated as absent", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, unitFile)
		if err := os.Symlink(filepath.Join(dir, "does-not-exist"), target); err != nil {
			t.Fatalf("creating dangling symlink: %v", err)
		}
		managed, err := IsSystemdManaged(dir, unitFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected not managed for a dangling symlink")
		}
	})

	t.Run("permission denied surfaces as error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("permission bits are not enforced for root")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		_, err := IsSystemdManaged(dir, unitFile)
		if err == nil {
			t.Fatal("expected error when the directory is unreadable, got nil")
		}
		if !strings.Contains(err.Error(), "checking systemd unit file") {
			t.Errorf("expected wrapped error, got: %v", err)
		}
	})
}

func TestIsLaunchdManaged(t *testing.T) {
	const plistFile = "org.elysiumlabs.eos.plist"

	t.Run("not installed", func(t *testing.T) {
		managed, err := IsLaunchdManaged(t.TempDir(), plistFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected not managed when plist is absent")
		}
	})

	t.Run("installed", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, plistFile), []byte("<plist/>"), 0644); err != nil {
			t.Fatalf("writing plist file: %v", err)
		}
		managed, err := IsLaunchdManaged(dir, plistFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !managed {
			t.Error("expected managed when plist exists")
		}
	})

	t.Run("dangling symlink treated as absent", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, plistFile)
		if err := os.Symlink(filepath.Join(dir, "does-not-exist"), target); err != nil {
			t.Fatalf("creating dangling symlink: %v", err)
		}
		managed, err := IsLaunchdManaged(dir, plistFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected not managed for a dangling symlink")
		}
	})

	t.Run("permission denied surfaces as error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("permission bits are not enforced for root")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		_, err := IsLaunchdManaged(dir, plistFile)
		if err == nil {
			t.Fatal("expected error when the directory is unreadable, got nil")
		}
		if !strings.Contains(err.Error(), "checking launchd plist file") {
			t.Errorf("expected wrapped error, got: %v", err)
		}
	})
}

// --- ResolveSystemdScope / ResolveLaunchdScope ---
//
// Legal matrix per the sum-type rule in STYLE.md: exactly one of
// {system-managed, user-managed, unmanaged-root-fallback, unmanaged-nonroot-fallback}
// holds at a time. Each case below asserts the other outcomes did not also fire.

func TestResolveSystemdScope(t *testing.T) {
	t.Run("system managed wins", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.WriteFile(filepath.Join(sysDir, SystemdTargetFileName), []byte(""), 0644); err != nil {
			t.Fatalf("writing system unit: %v", err)
		}

		dir, managed, userUnit, err := ResolveSystemdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != sysDir {
			t.Errorf("dir = %q, want %q", dir, sysDir)
		}
		if !managed {
			t.Error("expected managed = true")
		}
		if userUnit {
			t.Error("expected userUnit = false when the system unit wins")
		}
	})

	t.Run("user managed when system is not", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)
		userDir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(userDir, 0750); err != nil {
			t.Fatalf("creating user systemd dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(userDir, SystemdTargetFileName), []byte(""), 0644); err != nil {
			t.Fatalf("writing user unit: %v", err)
		}

		dir, managed, userUnit, err := ResolveSystemdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != userDir+"/" {
			t.Errorf("dir = %q, want %q", dir, userDir+"/")
		}
		if !managed {
			t.Error("expected managed = true")
		}
		if !userUnit {
			t.Error("expected userUnit = true when only the user unit is installed")
		}
	})

	t.Run("unmanaged falls back to privilege level", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)

		dir, managed, userUnit, err := ResolveSystemdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected managed = false when neither location has a unit installed")
		}
		if os.Getuid() == 0 {
			if dir != sysDir || userUnit {
				t.Errorf("root should fall back to system scope, got dir=%q userUnit=%v", dir, userUnit)
			}
		} else {
			wantUserDir := filepath.Join(home, ".config", "systemd", "user") + "/"
			if dir != wantUserDir || !userUnit {
				t.Errorf("non-root should fall back to user scope, got dir=%q userUnit=%v", dir, userUnit)
			}
		}
	})

	t.Run("home dir error propagates", func(t *testing.T) {
		sysDir := t.TempDir()
		t.Setenv("HOME", "")

		if _, _, _, err := ResolveSystemdScope(sysDir); err == nil {
			t.Fatal("expected error when the user systemd dir cannot be resolved, got nil")
		}
	})
}

func TestResolveLaunchdScope(t *testing.T) {
	t.Run("system managed wins", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.WriteFile(filepath.Join(sysDir, LaunchdPlistFileName), []byte(""), 0644); err != nil {
			t.Fatalf("writing system plist: %v", err)
		}

		dir, managed, userAgent, err := ResolveLaunchdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != sysDir {
			t.Errorf("dir = %q, want %q", dir, sysDir)
		}
		if !managed {
			t.Error("expected managed = true")
		}
		if userAgent {
			t.Error("expected userAgent = false when the system plist wins")
		}
	})

	t.Run("user managed when system is not", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)
		userDir := filepath.Join(home, "Library", "LaunchAgents")
		if err := os.MkdirAll(userDir, 0750); err != nil {
			t.Fatalf("creating user LaunchAgents dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(userDir, LaunchdPlistFileName), []byte(""), 0644); err != nil {
			t.Fatalf("writing user plist: %v", err)
		}

		dir, managed, userAgent, err := ResolveLaunchdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != userDir+"/" {
			t.Errorf("dir = %q, want %q", dir, userDir+"/")
		}
		if !managed {
			t.Error("expected managed = true")
		}
		if !userAgent {
			t.Error("expected userAgent = true when only the user plist is installed")
		}
	})

	t.Run("unmanaged falls back to privilege level", func(t *testing.T) {
		sysDir := t.TempDir()
		home := t.TempDir()
		t.Setenv("HOME", home)

		dir, managed, userAgent, err := ResolveLaunchdScope(sysDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected managed = false when neither location has a plist installed")
		}
		if os.Getuid() == 0 {
			if dir != sysDir || userAgent {
				t.Errorf("root should fall back to system scope, got dir=%q userAgent=%v", dir, userAgent)
			}
		} else {
			wantUserDir := filepath.Join(home, "Library", "LaunchAgents") + "/"
			if dir != wantUserDir || !userAgent {
				t.Errorf("non-root should fall back to user scope, got dir=%q userAgent=%v", dir, userAgent)
			}
		}
	})

	t.Run("home dir error propagates", func(t *testing.T) {
		sysDir := t.TempDir()
		t.Setenv("HOME", "")

		if _, _, _, err := ResolveLaunchdScope(sysDir); err == nil {
			t.Fatal("expected error when the user LaunchAgents dir cannot be resolved, got nil")
		}
	})
}

// --- CreateBaseDir filesystem-boundary cases ---

func TestCreateBaseDir_DanglingSymlinkAtTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "base")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), target); err != nil {
		t.Fatalf("creating dangling symlink: %v", err)
	}
	t.Setenv("EOS_BASE_DIR", target)

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	if _, err := CreateBaseDir(id); err == nil {
		t.Fatal("expected error when the base dir path is a dangling symlink, got nil")
	}
}

func TestCreateBaseDir_PermissionDeniedParent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission bits are not enforced for root")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0755) })

	dir := filepath.Join(parent, "base")
	t.Setenv("EOS_BASE_DIR", dir)

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	if _, err := CreateBaseDir(id); err == nil {
		t.Fatal("expected error when the parent directory is unwritable, got nil")
	}
}
