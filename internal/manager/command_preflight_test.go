package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestFirstCommandBinary(t *testing.T) {
	cases := []struct {
		command    string
		wantBinary string
		wantOK     bool
	}{
		{"npm start", "npm", true},
		{"npm", "npm", true},
		{"  npm   start  ", "npm", true},
		{"PORT=3000 npm start", "npm", true},
		{"FOO=bar BAZ=qux npm run x", "npm", true},
		{"", "", false},
		{"   ", "", false},
		{"PORT=3000", "", false},
		{"cd www && npm start", "", false},
		{"npm start | tee log.txt", "", false},
		{"npm start; echo done", "", false},
		{"./start.sh", "", false},
		{"bin/start", "", false},
		{`FOO="a b" npm start`, "", false},
		{"$HOME/bin/npm start", "", false},
		{"npm start > out.log", "", false},
		{"npm $(echo start)", "", false},
		{"exit 0", "", false},
		{"exit 1", "", false},
		{"cd /tmp", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			binary, ok := FirstCommandBinary(tc.command)
			if ok != tc.wantOK || binary != tc.wantBinary {
				t.Errorf("FirstCommandBinary(%q) = (%q, %v), want (%q, %v)", tc.command, binary, ok, tc.wantBinary, tc.wantOK)
			}
		})
	}
}

func TestIsEnvAssignment(t *testing.T) {
	cases := []struct {
		tok  string
		want bool
	}{
		{"VAR=1", true},
		{"VAR_NAME=1", true},
		{"VAR=", true},
		{"1VAR=1", false},
		{"=1", false},
		{"npm", false},
		{"VAR-NAME=1", false},
	}
	for _, tc := range cases {
		if got := isEnvAssignment(tc.tok); got != tc.want {
			t.Errorf("isEnvAssignment(%q) = %v, want %v", tc.tok, got, tc.want)
		}
	}
}

func TestBinaryInPathValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !binaryInPathValue("npm", dir) {
		t.Error("expected npm to be found on PATH")
	}
	if binaryInPathValue("npm", "/nonexistent/dir/99999") {
		t.Error("expected npm not to be found on a nonexistent PATH entry")
	}
	if binaryInPathValue("npm", "") {
		t.Error("expected npm not to be found on an empty PATH")
	}
	if !binaryInPathValue("npm", "/nonexistent/dir/99999"+string(os.PathListSeparator)+dir) {
		t.Error("expected npm to be found when one of several PATH entries has it")
	}
	if !binaryInPathValue("npm", string(os.PathListSeparator)+dir) {
		t.Error("expected a leading empty PATH entry to be skipped, not treated as a miss")
	}
}

func TestHostToolDirGlobs(t *testing.T) {
	cases := []struct {
		runtimeType string
		wantEmpty   bool
	}{
		{"node", false},
		{"nodejs", false},
		{"bun", false},
		{"deno", false},
		{"python", true},
		{"", true},
	}
	for _, tc := range cases {
		globs := hostToolDirGlobs(tc.runtimeType)
		if tc.wantEmpty && globs != nil {
			t.Errorf("hostToolDirGlobs(%q) = %v, want nil", tc.runtimeType, globs)
		}
		if !tc.wantEmpty && len(globs) == 0 {
			t.Errorf("hostToolDirGlobs(%q) returned no globs", tc.runtimeType)
		}
	}
}

func TestFindBinaryElsewhereOnHost_NoHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	if dir := findBinaryElsewhereOnHost("node", "npm"); dir != "" {
		t.Errorf("expected no match when the home directory can't be resolved, got %q", dir)
	}
}

func TestFindBinaryElsewhereOnHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if dir := findBinaryElsewhereOnHost("node", "npm"); dir != "" {
		t.Fatalf("expected no match before any nvm dir exists, got %q", dir)
	}
	if dir := findBinaryElsewhereOnHost("python", "pip"); dir != "" {
		t.Fatalf("expected no host tool dirs for an unmapped runtime type, got %q", dir)
	}

	nvmBinDir := filepath.Join(home, ".nvm", "versions", "node", "v20.11.0", "bin")
	if err := os.MkdirAll(nvmBinDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nvmBinDir, "npm"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := findBinaryElsewhereOnHost("node", "npm")
	if got != nvmBinDir {
		t.Errorf("findBinaryElsewhereOnHost() = %q, want %q", got, nvmBinDir)
	}

	if dir := findBinaryElsewhereOnHost("node", "does-not-exist-xyz"); dir != "" {
		t.Errorf("expected no match for a binary that isn't in the nvm dir either, got %q", dir)
	}
}

func TestCommandNotFoundError(t *testing.T) {
	t.Run("without a discoverable alternative", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		config := &types.ServiceConfig{Runtime: types.Runtime{Type: "node"}}
		err := commandNotFoundError("npm", config)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "npm") {
			t.Errorf("expected error to name the binary, got: %v", err)
		}
		if strings.Contains(err.Error(), "runtime.path") || strings.Contains(err.Error(), "runtime:") {
			t.Errorf("expected no runtime.path suggestion when nothing was found, got: %v", err)
		}
	})

	t.Run("with a discoverable alternative", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		voltaDir := filepath.Join(home, ".volta", "bin")
		if err := os.MkdirAll(voltaDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(voltaDir, "npm"), []byte("#!/bin/sh"), 0755); err != nil {
			t.Fatalf("write: %v", err)
		}

		config := &types.ServiceConfig{Runtime: types.Runtime{Type: "node"}}
		err := commandNotFoundError("npm", config)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "runtime:") || !strings.Contains(err.Error(), voltaDir) {
			t.Errorf("expected error to suggest runtime.path %q, got: %v", voltaDir, err)
		}
	})
}

func TestValidateCommandBinary(t *testing.T) {
	t.Run("passes when the binary is on PATH", func(t *testing.T) {
		serviceDir := t.TempDir()
		binDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh"), 0755); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Setenv("PATH", binDir)

		config := &types.ServiceConfig{Command: "npm start", Runtime: types.Runtime{Type: "node"}}
		if err := validateCommandBinary(config, serviceDir); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("fails when the binary is not on PATH", func(t *testing.T) {
		serviceDir := t.TempDir()
		t.Setenv("PATH", t.TempDir())
		t.Setenv("HOME", t.TempDir())

		config := &types.ServiceConfig{Command: "npm start", Runtime: types.Runtime{Type: "node"}}
		err := validateCommandBinary(config, serviceDir)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "npm") {
			t.Errorf("expected error to name npm, got: %v", err)
		}
	})

	t.Run("bails silently on a command it can't safely parse", func(t *testing.T) {
		serviceDir := t.TempDir()
		t.Setenv("PATH", t.TempDir())

		config := &types.ServiceConfig{Command: "cd www && npm start", Runtime: types.Runtime{Type: "node"}}
		if err := validateCommandBinary(config, serviceDir); err != nil {
			t.Errorf("expected a complex command to be skipped, got: %v", err)
		}
	})

	t.Run("bails silently when buildEnvironment itself would fail", func(t *testing.T) {
		serviceDir := t.TempDir()
		config := &types.ServiceConfig{Command: "npm start", EnvFile: "../outside-service-dir"}
		if err := validateCommandBinary(config, serviceDir); err != nil {
			t.Errorf("expected a buildEnvironment failure to be left for actual launch, got: %v", err)
		}
	})

	t.Run("runtime.path is enough to resolve a sibling binary", func(t *testing.T) {
		serviceDir := t.TempDir()
		runtimeDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(runtimeDir, "npm"), []byte("#!/bin/sh"), 0755); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Setenv("PATH", t.TempDir())

		config := &types.ServiceConfig{Command: "npm start", Runtime: types.Runtime{Type: "node", Path: runtimeDir}}
		if err := validateCommandBinary(config, serviceDir); err != nil {
			t.Errorf("expected runtime.path to put npm on PATH, got: %v", err)
		}
	})
}
