package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveRuntimePath(t *testing.T) {
	const home = "/home/me"
	for _, tc := range []struct {
		name, path, want string
		wantErr          bool
	}{
		{name: "empty", path: "", want: ""},
		{name: "absolute", path: "/opt/node/bin", want: "/opt/node/bin"},
		{name: "bare tilde", path: "~", want: home},
		{name: "tilde slash", path: "~/", want: home},
		{name: "tilde-relative", path: "~/node/bin", want: "/home/me/node/bin"},
		{name: "home-relative", path: "node/bin", want: "/home/me/node/bin"},
		{name: "tilde user", path: "~bob/node/bin", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRuntimePath(tc.path, home)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// writeServiceYAML writes a minimal service.yaml into dir and returns its path.
func writeServiceYAML(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(path, []byte("name: cms\ncommand: node\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Both loaders hand back an absolute runtime.path, so every consumer
// (validation, launch, eos info) sees the same value.
func TestLoadersResolveRuntimePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := writeServiceYAML(t, t.TempDir(), "runtime:\n  type: node\n  path: ~/node/bin\n")
	want := filepath.Join(home, "node", "bin")

	loaded, err := LoadServiceConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadServiceConfig: %v", err)
	}
	if loaded.Runtime.Path != want {
		t.Errorf("LoadServiceConfig runtime.path = %q, want %q", loaded.Runtime.Path, want)
	}

	parsed, err := cfgvLoadAndParseConfig(cfgPath)
	if err != nil {
		t.Fatalf("cfgvLoadAndParseConfig: %v", err)
	}
	if parsed.Runtime.Path != want {
		t.Errorf("cfgvLoadAndParseConfig runtime.path = %q, want %q", parsed.Runtime.Path, want)
	}
}

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
			config, err := LoadServiceConfig(writeServiceYAML(t, serviceDir, "runtime:\n  type: node\n  path: "+tc.path+"\n"))
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if err = ValidateRuntimeBinary(config.Runtime); err != nil {
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
	absolute := t.TempDir()
	for _, tc := range []struct {
		name, path string
		wantError  bool
	}{
		{"empty", "", false},
		{"absolute", absolute, false},
		{"home-relative", "node/bin", true},
		{"tilde-relative", "~/node/bin", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := ""
			if tc.path != "" {
				body = "runtime:\n  path: " + tc.path + "\n"
			}
			config, err := LoadServiceConfig(writeServiceYAML(t, t.TempDir(), body))
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "home directory") {
					t.Fatalf("expected a home directory error loading %q, got %v", tc.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected HOME dependency: %v", err)
			}
			if config.Runtime.Path != tc.path {
				t.Errorf("runtime.path = %q, want %q unchanged", config.Runtime.Path, tc.path)
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
	config, err := LoadServiceConfig(writeServiceYAML(t, serviceDir, "runtime:\n  path: ~/node/bin\nenv_file: .env\nport: 4321\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
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
