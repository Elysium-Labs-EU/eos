package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
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
	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n")
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

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n")
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

	const wantHeader = "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json"
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
	// no y/n prompt; --force skips it
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
	// advanced mode; press enter on runtime fields to accept detected defaults
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

func TestInitCmd_RuntimeDetection_Deno(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deno.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// advanced mode; press enter on runtime fields to accept detected defaults
	root.SetIn(&slowReader{strings.NewReader("svc\nmain.ts\na\n\n\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if !strings.Contains(string(data), "deno") {
		t.Errorf("expected deno runtime in output, got: %s", string(data))
	}
}

func TestInitCmd_RuntimeDetection_Python(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("setup pyproject.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".python-version"), []byte("3.11.4\n"), 0644); err != nil {
		t.Fatalf("setup .python-version: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("api\nmain.py\na\n\n\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if !strings.Contains(string(data), "3.11.4") {
		t.Errorf("expected python version in runtime path, got: %s", string(data))
	}
}

func TestInitCmd_RuntimeDetection_NodeVersionNoPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("setup package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".node-version"), []byte("18.0.0\n"), 0644); err != nil {
		t.Fatalf("setup .node-version: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("api\nserver.js\na\n\n\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if !strings.Contains(string(data), "v18.0.0") {
		t.Errorf("expected v-prefixed node version in runtime path, got: %s", string(data))
	}
}

func TestInitCmd_RuntimeDetection_NodeNoVersionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("setup package.json: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// name, command, mode, port, runtime type=node, runtime path blank (no pinned version to suggest)
	root.SetIn(&slowReader{strings.NewReader("api\nserver.js\na\n\nnode\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n")
	var cfg types.ServiceConfig
	if err := yaml.Unmarshal([]byte(yamlOnly), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Runtime.Type != "" {
		t.Errorf("expected no runtime set when path is blank, got type %q", cfg.Runtime.Type)
	}
}

func TestInitCmd_RuntimeDetection_PythonNoVersionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(""), 0644); err != nil {
		t.Fatalf("setup requirements.txt: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("api\nmain.py\na\n\npython3\n\n\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "service.yaml"))
	if err != nil {
		t.Fatalf("service.yaml not written: %v", err)
	}

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n")
	var cfg types.ServiceConfig
	if err := yaml.Unmarshal([]byte(yamlOnly), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Runtime.Type != "" {
		t.Errorf("expected no runtime set when path is blank, got type %q", cfg.Runtime.Type)
	}
}

func TestInitCmd_WriteFile_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission checks do not apply")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer func() {
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}()

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("svc\napp.js\ns\n\n")})
	root.SetArgs([]string{"init", dir})

	if err := root.ExecuteContext(t.Context()); err == nil {
		t.Fatalf("expected error writing to read-only directory, got nil")
	}
	if !strings.Contains(buf.String(), "writing file") {
		t.Errorf("expected 'writing file' error in output, got: %s", buf.String())
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
	// blank command; should be accepted and file written with empty command
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

	yamlOnly := strings.TrimPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n")
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

// TestInitCmd_NonexistentDir covers the os.Stat/os.IsNotExist branch
// (cmd/init.go:62-65): pointing init at a directory that doesn't exist must
// fail up front, before any prompting starts.
func TestInitCmd_NonexistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"init", dir})

	err := root.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(buf.String(), "directory does not exist") {
		t.Errorf("expected 'directory does not exist', got: %s", buf.String())
	}
}

// TestInitCmd_WriteFileError covers the os.WriteFile error branch
// (cmd/init.go:126-129). service.yaml is pre-created as a *directory* at the
// output path rather than a regular file: os.Stat still succeeds so init
// proceeds past the exists/overwrite prompt (satisfied here via --force), but
// the final os.WriteFile of the generated config fails because its target is
// a directory, not a file — a real, permission-independent failure mode.
func TestInitCmd_WriteFileError(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "service.yaml")
	if err := os.Mkdir(outputPath, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	db, _, tmpBase := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tmpBase, t.Context(), testutil.NewTestLogger(t))
	root := newTestRootCmd(mgr)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(&slowReader{strings.NewReader("svc\napp.js\ns\n\n")})
	root.SetArgs([]string{"init", "--force", dir})

	err := root.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(buf.String(), "writing file") {
		t.Errorf("expected 'writing file' error, got: %s", buf.String())
	}
}

// Not tested, and why:
//
// cmd/init.go:58 (filepath.Abs error resolving the target directory) —
// same as add.go's identical pattern: filepath.Abs only fails when
// os.Getwd() fails, which would require deleting the test binary's own
// working directory — an unsafe, non-isolated mutation of global process
// state shared with every other test in this package.
//
// cmd/init.go:122 (yaml.Marshal error) — initServiceConfig is a plain
// struct of strings, ints, and a pointer to another plain struct; yaml.v3
// cannot fail marshaling it (no channels, funcs, or cyclic references are
// possible here), so this branch is unreachable dead code from any real
// input the command can construct.
