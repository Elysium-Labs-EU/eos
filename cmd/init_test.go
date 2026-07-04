package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestInitCmd_SimpleMode(t *testing.T) {
	dir := t.TempDir()
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// service name (blank = dirname), command, mode=simple, port
	root.SetIn(&slowReader{strings.NewReader("\nstart.sh\ns\n3000\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputPath := filepath.Join(dir, "service.yaml")
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	var cfg types.ServiceConfig
	// strip schema header comment before unmarshaling
	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json\n\n")
	if err := yaml.Unmarshal([]byte(yamlOnly), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Name != filepath.Base(dir) {
		t.Errorf("name: got %q, want %q", cfg.Name, filepath.Base(dir))
	}
	if cfg.Command != "start.sh" {
		t.Errorf("command: got %q, want %q", cfg.Command, "start.sh")
	}
	if cfg.Port != 3000 {
		t.Errorf("port: got %d, want 3000", cfg.Port)
	}
	if cfg.Runtime.Type != "" {
		t.Errorf("simple mode should not set runtime, got %q", cfg.Runtime.Type)
	}
}

func TestInitCmd_AdvancedMode(t *testing.T) {
	dir := t.TempDir()
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("api\nserver.js\na\n3000\nnodejs\n~/.nvm/bin\n.env\n512\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json\n\n")
	var cfg types.ServiceConfig
	if err := yaml.Unmarshal([]byte(yamlOnly), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Name != "api" {
		t.Errorf("name: got %q, want api", cfg.Name)
	}
	if cfg.Command != "server.js" {
		t.Errorf("command: got %q", cfg.Command)
	}
	if cfg.Port != 3000 {
		t.Errorf("port: got %d, want 3000", cfg.Port)
	}
	if cfg.Runtime.Type != "nodejs" {
		t.Errorf("runtime type: got %q, want nodejs", cfg.Runtime.Type)
	}
	if cfg.Runtime.Path != "~/.nvm/bin" {
		t.Errorf("runtime path: got %q, want ~/.nvm/bin", cfg.Runtime.Path)
	}
	if cfg.EnvFile != ".env" {
		t.Errorf("env_file: got %q, want .env", cfg.EnvFile)
	}
	if cfg.MemoryLimitMb != 512 {
		t.Errorf("memory_limit_mb: got %d, want 512", cfg.MemoryLimitMb)
	}
}

func TestInitCmd_SchemaHeader(t *testing.T) {
	dir := t.TempDir()
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("svc\napp.js\ns\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	const wantHeader = "# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json"
	firstLine, _, _ := strings.Cut(string(data), "\n")
	if firstLine != wantHeader {
		t.Errorf("schema header: got %q", firstLine)
	}
}

func TestInitCmd_ExistingFile_Decline(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "service.yaml")
	original := []byte("name: original\ncommand: original.sh\n")
	if err := os.WriteFile(outputPath, original, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// file must be unchanged
	got, _ := os.ReadFile(outputPath)
	if string(got) != string(original) {
		t.Errorf("file was modified on decline")
	}
	if !strings.Contains(buf.String(), "init canceled") {
		t.Errorf("expected 'init canceled' in output, got: %s", buf.String())
	}
}

func TestInitCmd_ExistingFile_Confirm(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(outputPath, []byte("name: old\ncommand: old.sh\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("y\nnewsvc\nnew.sh\ns\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "newsvc") {
		t.Errorf("file not overwritten, still has old content: %s", string(data))
	}
}

func TestInitCmd_Force(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(outputPath, []byte("name: old\ncommand: old.sh\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// no y/n prompt — --force skips it
	root.SetIn(&slowReader{strings.NewReader("forced\nforced.sh\ns\n\n")})
	root.SetArgs([]string{"init", "--force", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(data), "forced") {
		t.Errorf("--force did not overwrite file: %s", string(data))
	}
}

func TestInitCmd_RuntimeDetection_Bun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte(""), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// advanced mode — press enter on runtime fields to accept detected defaults
	root.SetIn(&slowReader{strings.NewReader("svc\nindex.ts\na\n\n\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if !strings.Contains(string(data), "bun") {
		t.Errorf("expected bun runtime in output, got: %s", string(data))
	}
}

func TestInitCmd_RuntimeDetection_NVM(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("setup package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("v20.1.0\n"), 0644); err != nil {
		t.Fatalf("setup .nvmrc: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// advanced mode, accept all defaults with Enter
	root.SetIn(&slowReader{strings.NewReader("api\nserver.js\na\n\n\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if !strings.Contains(string(data), "v20.1.0") {
		t.Errorf("expected nvm version in runtime path, got: %s", string(data))
	}
}

func TestInitCmd_SkippedCommand(t *testing.T) {
	dir := t.TempDir()
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// blank command — should be accepted and file written with empty command
	root.SetIn(&slowReader{strings.NewReader("svc\n\ns\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputPath := filepath.Join(dir, "service.yaml")
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json\n\n")
	var cfg types.ServiceConfig
	if err := yaml.Unmarshal([]byte(yamlOnly), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Command != "" {
		t.Errorf("skipped command: got %q, want empty", cfg.Command)
	}
}

func TestInitCmd_NextStep(t *testing.T) {
	dir := t.TempDir()
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("svc\napp.js\ns\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFlag := "eos run -f"
	wantPath := filepath.Join(dir, "service.yaml")
	out := buf.String()
	if !strings.Contains(out, wantFlag) {
		t.Errorf("next step missing %q, got: %s", wantFlag, out)
	}
	if !strings.Contains(out, wantPath) {
		t.Errorf("next step missing path %q, got: %s", wantPath, out)
	}
}
