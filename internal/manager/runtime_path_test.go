package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestRuntimePathValidationAndLaunchAgree(t *testing.T) {
	home := t.TempDir()
	serviceDir := t.TempDir()
	ambientPath := t.TempDir()
	runtimeDir := filepath.Join(home, "node", "bin")
	decoyDir := filepath.Join(serviceDir, "node", "bin")
	for _, dir := range []string{runtimeDir, decoyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "node"), []byte("#!/bin/sh\nprintf 'home runtime\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyDir, "node"), []byte("#!/bin/sh\nprintf 'service runtime\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", ambientPath)

	for _, tc := range []struct{ name, path string }{
		{"absolute", runtimeDir},
		{"home-relative", "node/bin"},
		{"tilde-relative", "~/node/bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &types.ServiceConfig{Command: "node", Runtime: types.Runtime{Type: "node", Path: tc.path}}
			if err := ValidateRuntimeBinary(config.Runtime); err != nil {
				t.Fatalf("validate runtime: %v", err)
			}
			env, err := buildEnvironment(config, serviceDir)
			if err != nil {
				t.Fatalf("build environment: %v", err)
			}
			wantPath := "PATH=" + runtimeDir + string(os.PathListSeparator) + ambientPath
			if !slices.Contains(env, wantPath) {
				t.Errorf("launch environment does not contain %q", wantPath)
			}
			if err = validateCommandBinary(config, serviceDir); err != nil {
				t.Errorf("command preflight: %v", err)
			}
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exec node")
			cmd.Dir = serviceDir
			cmd.Env = env
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("launch runtime: %v: %s", err, output)
			}
			if string(output) != "home runtime\n" {
				t.Errorf("launched the wrong runtime: %q", output)
			}
		})
	}
}

func TestRuntimePathMissingHome(t *testing.T) {
	t.Setenv("HOME", "")
	ambientPath := t.TempDir()
	t.Setenv("PATH", ambientPath)
	for _, tc := range []struct {
		name, path string
		wantError  bool
	}{
		{"empty", "", false},
		{"absolute", t.TempDir(), false},
		{"home-relative", "node/bin", true},
		{"tilde-relative", "~/node/bin", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &types.ServiceConfig{Runtime: types.Runtime{Path: tc.path}}
			env, err := buildEnvironment(config, t.TempDir())
			if tc.wantError {
				if err == nil {
					t.Fatal("expected an error resolving the runtime without HOME")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected HOME dependency: %v", err)
			}
			wantPath := "PATH=" + ambientPath
			if tc.path != "" {
				wantPath = "PATH=" + tc.path + string(os.PathListSeparator) + ambientPath
			}
			if !slices.Contains(env, wantPath) {
				t.Errorf("launch environment does not contain %q", wantPath)
			}
		})
	}
}

func TestRuntimePathPreservesEnvironmentOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	serviceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(serviceDir, ".env"), []byte("PATH=/service/bin\nSERVICE_MODE=test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := &types.ServiceConfig{
		Runtime: types.Runtime{Path: "~/node/bin"},
		EnvFile: ".env",
		Port:    4321,
	}
	env, err := buildEnvironment(config, serviceDir)
	if err != nil {
		t.Fatalf("build environment: %v", err)
	}
	for _, want := range []string{"PATH=/service/bin", "SERVICE_MODE=test", "PORT=4321"} {
		if !slices.Contains(env, want) {
			t.Errorf("launch environment does not contain %q", want)
		}
	}
}
