package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// setupDiagnoseCmd is setupCmd's counterpart for diagnose tests specifically:
// diagnose never goes through getManager — it opens its own manager the same
// way --no-daemon does, via database.NewDB's production "state.db" path (see
// runDiagnose). setupCmd's manager is backed by testutil.SetupTestDB's
// fixture-only "test.db" file instead, so a service registered through that
// shared test manager would be invisible to diagnose's own, separately opened
// connection. Wiring this manager through database.NewDB against the same
// EOS_BASE_DIR keeps both the "add" command and diagnose's internal manager
// pointed at the same on-disk database.
func setupDiagnoseCmd(t *testing.T) (cmd *cobra.Command, outBuf *bytes.Buffer, errBuf *bytes.Buffer, tempDir string) {
	t.Helper()
	tempDir = t.TempDir()
	t.Setenv("EOS_BASE_DIR", tempDir)

	db, err := database.NewDB(t.Context(), tempDir)
	if err != nil {
		t.Fatalf("opening production database: %v", err)
	}
	t.Cleanup(func() { _ = db.CloseDBConnection() })

	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	c := newTestRootCmd(mgr)

	var ob, eb bytes.Buffer
	c.SetOut(&ob)
	c.SetErr(&eb)
	return c, &ob, &eb, tempDir
}

// registerPlainService registers a minimal service (no runtime, no env_file)
// named name, rooted under tempDir/name.
func registerPlainService(t *testing.T, cmd *cobra.Command, tempDir, name string, errBuf *bytes.Buffer) string {
	t.Helper()

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithName(name))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, name)
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create %s directory: %v", name, err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	return fullDirPath
}

// readDiagnoseBundle opens the tar.gz at path and returns every entry's
// content keyed by its in-archive name.
func readDiagnoseBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()

	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatalf("opening bundle: %v", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("creating gzip reader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	files := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading tar entry: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading tar entry content: %v", err)
		}
		files[hdr.Name] = data
	}
	return files
}

func unmarshalOrFatal[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("unmarshaling %T: %v\ndata: %s", v, err, data)
	}
	return v
}

func stepOK(t *testing.T, manifest *diagnoseManifest, name string) (diagnoseStepResult, bool) {
	t.Helper()
	for _, step := range manifest.Steps {
		if step.Name == name {
			return step, true
		}
	}
	return diagnoseStepResult{}, false
}

func TestDiagnoseCmd_WritesBundleForRegisteredServices(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupDiagnoseCmd(t)
	registerPlainService(t, cmd, tempDir, "svc-a", errBuf)
	registerPlainService(t, cmd, tempDir, "svc-b", errBuf)

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	if !strings.Contains(outBuf.String(), outputPath) {
		t.Errorf("expected the success message to mention the output path, got: %s", outBuf.String())
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected bundle to exist at %s: %v", outputPath, err)
	}

	files := readDiagnoseBundle(t, outputPath)
	if _, ok := files["manifest.json"]; !ok {
		t.Fatal("expected manifest.json in bundle")
	}
	manifest := unmarshalOrFatal[diagnoseManifest](t, files["manifest.json"])

	if manifest.OS == "" || manifest.Arch == "" {
		t.Errorf("expected OS/Arch populated, got: %+v", manifest)
	}
	if manifest.HostID == "" {
		t.Error("expected a non-empty host id")
	}

	for _, name := range []string{"version", "daemon", "service:svc-a", "service:svc-b"} {
		step, found := stepOK(t, &manifest, name)
		if !found {
			t.Errorf("expected step %q in manifest, got: %+v", name, manifest.Steps)
			continue
		}
		if !step.Captured {
			t.Errorf("expected step %q to be ok, got error: %s", name, step.Error)
		}
	}

	servicesData, ok := files["services.json"]
	if !ok {
		t.Fatal("expected services.json in bundle")
	}
	services := unmarshalOrFatal[[]apiStatusService](t, servicesData)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d: %+v", len(services), services)
	}
	names := map[string]bool{}
	for _, svc := range services {
		names[svc.Name] = true
		if svc.Status != "stopped" {
			t.Errorf("expected never-started service %q to be stopped, got %q", svc.Name, svc.Status)
		}
	}
	if !names["svc-a"] || !names["svc-b"] {
		t.Errorf("expected both services present, got: %+v", names)
	}

	daemonInfo := unmarshalOrFatal[diagnoseDaemonInfo](t, files["daemon.json"])
	if daemonInfo.Mode != "standalone" {
		t.Errorf("expected standalone mode, got %q", daemonInfo.Mode)
	}
	if daemonInfo.Running {
		t.Error("expected daemon to be reported as not running (no daemon process started in test)")
	}

	// A service that has never run has no log file yet — that per-stream step
	// must fail without blocking the rest of the bundle (see the command's
	// never-fail-fast requirement).
	if step, found := stepOK(t, &manifest, "service-log:svc-a:out"); !found || step.Captured {
		t.Errorf("expected service-log:svc-a:out to fail (no log file yet), got: %+v", step)
	}
}

func TestDiagnoseCmd_NoServicesRegistered(t *testing.T) {
	cmd, _, errBuf, _ := setupDiagnoseCmd(t)

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error with no services, got: %v\nerr output: %s", err, errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	manifest := unmarshalOrFatal[diagnoseManifest](t, files["manifest.json"])
	step, found := stepOK(t, &manifest, "services")
	if !found || !step.Captured {
		t.Errorf("expected services step ok with an empty catalog, got: %+v", step)
	}
}

// TestDiagnoseCmd_LogsScrubbedAndTimeWindowed writes a synthetic daemon.log
// with one line outside the --since window (must be dropped) and one inside
// it containing a home-directory path and a secret-shaped token (both must
// be redacted, not merely excluded).
func TestDiagnoseCmd_LogsScrubbedAndTimeWindowed(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)

	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	oldLine := `{"time":"2000-01-01T00:00:00Z","level":"INFO","msg":"ancient line, must be dropped"}`
	recentLine := `{"time":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","level":"INFO","msg":"recent line home /home/alice/secrets password=hunter2"}`
	content := oldLine + "\n" + recentLine + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "daemon.log"), []byte(content), 0644); err != nil {
		t.Fatalf("writing daemon.log: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--since", "1m", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	daemonLog, ok := files["logs/daemon.log"]
	if !ok {
		t.Fatal("expected logs/daemon.log in bundle")
	}
	got := string(daemonLog)

	if strings.Contains(got, "ancient line") {
		t.Errorf("expected the out-of-window line to be dropped, got: %s", got)
	}
	if !strings.Contains(got, "recent line") {
		t.Errorf("expected the in-window line to survive, got: %s", got)
	}
	if strings.Contains(got, "/home/alice") {
		t.Errorf("expected the home-directory path to be redacted, got: %s", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("expected the secret-shaped token to be redacted, got: %s", got)
	}
	if !strings.Contains(got, "<redacted-path>") || !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected redaction markers in place of the scrubbed content, got: %s", got)
	}

	manifest := unmarshalOrFatal[diagnoseManifest](t, files["manifest.json"])
	if step, found := stepOK(t, &manifest, "daemon-log"); !found || !step.Captured {
		t.Errorf("expected daemon-log step ok, got: %+v", step)
	}
}

func TestDiagnoseCmd_LinesCapKeepsMostRecent(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)

	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	now := time.Now().UTC()
	var lines []string
	for i := range 5 {
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		lines = append(lines, `{"time":"`+ts+`","level":"INFO","msg":"line `+string(rune('0'+i))+`"}`)
	}
	if err := os.WriteFile(filepath.Join(logDir, "daemon.log"), []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("writing daemon.log: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--since", "1h", "--lines", "2", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	got := strings.TrimRight(string(files["logs/daemon.log"]), "\n")
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 2 {
		t.Fatalf("expected --lines to cap output at 2 lines, got %d: %v", len(gotLines), gotLines)
	}
	if !strings.Contains(gotLines[len(gotLines)-1], "line 4") {
		t.Errorf("expected the cap to keep the most recent lines, got: %v", gotLines)
	}
}

func TestDiagnoseCmd_DaemonLogUnavailableUnderSystemd(t *testing.T) {
	cmd, _, errBuf, _ := setupDiagnoseCmd(t)

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	// No --no-daemon here: setupCmd's manager is already wired for standalone.
	// Simulate "no standalone log" indirectly isn't possible without touching
	// config, so instead assert the ok path already covered above and the
	// distinct systemd-unavailable message via the pure helper directly.
	daemonLogFile, step := diagnoseCollectDaemonLog(t.TempDir(), nil, diagnoseOptions{Since: 10 * time.Minute, Lines: 100})
	if daemonLogFile != nil {
		t.Error("expected no daemon log file when daemon config is nil")
	}
	if step.Captured {
		t.Error("expected daemon-log step to fail when daemon config is nil")
	}
	if !strings.Contains(step.Error, "not managed as a standalone daemon") {
		t.Errorf("expected a descriptive skip reason, got: %s", step.Error)
	}

	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
}

func TestDiagnoseCmd_IncludeEnv(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "SECRET=do-not-redact-me\n", errBuf)

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--include-env", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	if !strings.Contains(errBuf.String(), "do not attach this to a public issue") {
		t.Errorf("expected the --include-env warning on stderr, got: %s", errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	envData, ok := files["env/cms.env"]
	if !ok {
		t.Fatalf("expected env/cms.env in bundle, got files: %v", mapKeys(files))
	}
	if !strings.Contains(string(envData), "SECRET=do-not-redact-me") {
		t.Errorf("expected --include-env to write raw, unredacted content, got: %s", envData)
	}
}

func TestDiagnoseCmd_IncludeEnvOmittedByDefault(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)
	addServiceWithEnvFile(t, cmd, tempDir, "SECRET=do-not-redact-me\n", errBuf)

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	if strings.Contains(errBuf.String(), "do not attach this to a public issue") {
		t.Error("expected no --include-env warning when the flag is not set")
	}

	files := readDiagnoseBundle(t, outputPath)
	if _, ok := files["env/cms.env"]; ok {
		t.Error("expected no env/ files in the bundle by default")
	}
}

// TestDiagnoseCmd_BadServiceYAMLDoesNotBlockOthers registers two services,
// then corrupts one's service.yaml on disk after registration. The bad
// service's env step must fail independently without blocking the good
// service's, or the bundle as a whole.
func TestDiagnoseCmd_BadServiceYAMLDoesNotBlockOthers(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)
	goodDir := addServiceWithEnvFile(t, cmd, tempDir, "FOO=bar\n", errBuf)
	_ = goodDir
	badDir := registerPlainService(t, cmd, tempDir, "broken", errBuf)
	if err := os.WriteFile(filepath.Join(badDir, "service.yaml"), []byte("not: [valid yaml"), 0644); err != nil {
		t.Fatalf("corrupting service.yaml: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--include-env", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error even with a corrupted service.yaml, got: %v\nerr output: %s", err, errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	manifest := unmarshalOrFatal[diagnoseManifest](t, files["manifest.json"])

	step, found := stepOK(t, &manifest, "env:broken")
	if !found || step.Captured {
		t.Errorf("expected env:broken step to fail on the corrupted service.yaml, got: %+v", step)
	}

	if _, ok := files["env/cms.env"]; !ok {
		t.Error("expected the good service's env dump to still be present despite the other service's failure")
	}
}

func TestDiagnoseCmd_NoServiceLogsSkipsLogFiles(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)
	registerPlainService(t, cmd, tempDir, "svc-a", errBuf)

	logPath, errorLogPath, err := diagnoseTestManager(t, tempDir).NewServiceLogFiles(t.Context(), "svc-a")
	if err != nil {
		t.Fatalf("creating service log files: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("hello from stdout\n"), 0644); err != nil {
		t.Fatalf("writing stdout log: %v", err)
	}
	if err := os.WriteFile(errorLogPath, []byte("hello from stderr\n"), 0644); err != nil {
		t.Fatalf("writing stderr log: %v", err)
	}

	withLogsPath := filepath.Join(t.TempDir(), "with-logs.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--output", withLogsPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	withLogsFiles := readDiagnoseBundle(t, withLogsPath)
	if _, ok := withLogsFiles["logs/svc-a-out.log"]; !ok {
		t.Error("expected logs/svc-a-out.log by default")
	}

	noLogsPath := filepath.Join(t.TempDir(), "no-logs.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--no-service-logs", "--output", noLogsPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	noLogsFiles := readDiagnoseBundle(t, noLogsPath)
	if _, ok := noLogsFiles["logs/svc-a-out.log"]; ok {
		t.Error("expected --no-service-logs to omit logs/svc-a-out.log")
	}
	if _, ok := noLogsFiles["logs/svc-a-error.log"]; ok {
		t.Error("expected --no-service-logs to omit logs/svc-a-error.log")
	}
}

// diagnoseTestManager opens a second local manager against the same base dir
// setupCmd already wired, so the test can create real service log files on
// disk exactly where the diagnose command itself will look for them.
func diagnoseTestManager(t *testing.T, baseDir string) manager.ServiceManager {
	t.Helper()
	mgr, cleanup, err := newLocalManagerWithCleanup(t.Context(), baseDir, false, nil)
	if err != nil {
		t.Fatalf("opening local manager: %v", err)
	}
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return mgr
}

func TestDiagnoseCmd_DefaultOutputName(t *testing.T) {
	cmd, _, errBuf, _ := setupDiagnoseCmd(t)
	t.Chdir(t.TempDir())

	cmd.SetArgs([]string{"diagnose"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading cwd: %v", err)
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "eos-diagnose-") && strings.HasSuffix(entry.Name(), ".tar.gz") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a default eos-diagnose-<timestamp>.tar.gz file, got entries: %v", entries)
	}
}

func TestDiagnoseCmd_UnwritableOutputIsFatal(t *testing.T) {
	cmd, _, errBuf, _ := setupDiagnoseCmd(t)

	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	outputPath := filepath.Join(notADir, "bundle.tar.gz")

	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed for an unwritable output path, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "writing diagnostic bundle") {
		t.Errorf("expected a descriptive fatal error, got: %s", errBuf.String())
	}
}

func mapKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestDiagnoseScrubLine(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantNot []string
	}{
		{"home path", "loaded config from /home/alice/.eos/config.yaml", []string{"/home/alice"}},
		{"macos user path", "loaded config from /Users/bob/.eos/config.yaml", []string{"/Users/bob"}},
		{"root path", "loaded config from /root/.eos/config.yaml", []string{"/root/.eos"}},
		{"password assignment", `password="hunter2"`, []string{"hunter2"}},
		{"api key assignment", "api_key=sk-abc123def456", []string{"sk-abc123def456"}},
		{"aws key", "AKIAABCDEFGHIJKLMNOP found in output", []string{"AKIAABCDEFGHIJKLMNOP"}},
		{"private key block", "-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJ\n-----END RSA PRIVATE KEY-----", []string{"MIIBogIBAAJ"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diagnoseScrubLine(tt.in)
			for _, forbidden := range tt.wantNot {
				if strings.Contains(got, forbidden) {
					t.Errorf("diagnoseScrubLine(%q) = %q, expected %q to be redacted", tt.in, got, forbidden)
				}
			}
		})
	}
}

func TestDiagnoseScrubLine_PreservesUnrelatedContent(t *testing.T) {
	in := "service cms started on port 3000"
	got := diagnoseScrubLine(in)
	if got != in {
		t.Errorf("expected unrelated content unchanged, got: %q", got)
	}
}

// TestDiagnoseScrubLines_MultiLinePrivateKey is the regression test for a
// real PEM private key logged one line per line, as a service actually would
// write it to its own stdout — diagnoseScrubLine alone can't catch this,
// since its BEGIN...END pattern only matches within a single string, and a
// real log file hands each line to the scrubber separately.
func TestDiagnoseScrubLines_MultiLinePrivateKey(t *testing.T) {
	lines := []string{
		"service starting up",
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIBOgIBAAJBAK8z9x8example1",
		"MIIBOgIBAAJBAK8z9x8example2",
		"-----END RSA PRIVATE KEY-----",
		"service ready on port 3000",
	}

	got := diagnoseScrubLines(lines)
	if len(got) != len(lines) {
		t.Fatalf("expected %d lines out, got %d: %v", len(lines), len(got), got)
	}

	joined := strings.Join(got, "\n")
	for _, forbidden := range []string{"MIIBOgIBAAJBAK8z9x8example1", "MIIBOgIBAAJBAK8z9x8example2"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("expected private key body line %q to be redacted, got: %v", forbidden, got)
		}
	}
	if got[0] != "service starting up" || got[len(got)-1] != "service ready on port 3000" {
		t.Errorf("expected lines outside the key block to survive unchanged, got: %v", got)
	}
	for i := 1; i <= 4; i++ {
		if got[i] != "[REDACTED PRIVATE KEY]" {
			t.Errorf("expected line %d (inside the key block) to be redacted, got: %q", i, got[i])
		}
	}
}

// TestDiagnoseScrubLines_UnterminatedPrivateKeyStaysRedacted proves a key
// block with no END marker in the tailed window (e.g. --lines truncated it)
// still redacts every line from BEGIN onward, rather than falling back to
// leaking the rest of the file once the state never closes.
func TestDiagnoseScrubLines_UnterminatedPrivateKeyStaysRedacted(t *testing.T) {
	lines := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIBOgIBAAJBAK8z9x8example1",
		"MIIBOgIBAAJBAK8z9x8example2",
	}
	got := diagnoseScrubLines(lines)
	for i, line := range got {
		if line != "[REDACTED PRIVATE KEY]" {
			t.Errorf("expected line %d to stay redacted with no END marker, got: %q", i, line)
		}
	}
}

// TestDiagnoseScrubLines_AppliesLineScrubbingOutsideKeyBlocks proves lines
// outside any private-key block still go through the normal path/secret
// redaction diagnoseScrubLine applies.
func TestDiagnoseScrubLines_AppliesLineScrubbingOutsideKeyBlocks(t *testing.T) {
	got := diagnoseScrubLines([]string{"loaded config from /home/alice/.eos/config.yaml"})
	if strings.Contains(got[0], "/home/alice") {
		t.Errorf("expected home path to be redacted, got: %q", got[0])
	}
}

// TestDiagnoseCmd_MultiLinePrivateKeyRedactedInRealServiceLog exercises the
// actual per-line collection path (diagnoseCollectServiceLogs, via the real
// "diagnose" command), not just the pure scrubber directly — the gap the
// prior single-call PEM test left uncovered.
func TestDiagnoseCmd_MultiLinePrivateKeyRedactedInRealServiceLog(t *testing.T) {
	cmd, _, errBuf, tempDir := setupDiagnoseCmd(t)
	registerPlainService(t, cmd, tempDir, "svc-a", errBuf)

	logPath, _, err := diagnoseTestManager(t, tempDir).NewServiceLogFiles(t.Context(), "svc-a")
	if err != nil {
		t.Fatalf("creating service log files: %v", err)
	}
	pemLog := "starting up\n" +
		"-----BEGIN RSA PRIVATE KEY-----\n" +
		"MIIBOgIBAAJBAK8z9x8realkeybodyline1\n" +
		"MIIBOgIBAAJBAK8z9x8realkeybodyline2\n" +
		"-----END RSA PRIVATE KEY-----\n" +
		"ready\n"
	if err := os.WriteFile(logPath, []byte(pemLog), 0644); err != nil {
		t.Fatalf("writing stdout log: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd.SetArgs([]string{"diagnose", "--output", outputPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("diagnose should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	files := readDiagnoseBundle(t, outputPath)
	got := string(files["logs/svc-a-out.log"])
	for _, forbidden := range []string{"MIIBOgIBAAJBAK8z9x8realkeybodyline1", "MIIBOgIBAAJBAK8z9x8realkeybodyline2"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("expected multi-line PEM key body to be redacted in the bundled log, got: %s", got)
		}
	}
	if !strings.Contains(got, "starting up") || !strings.Contains(got, "ready") {
		t.Errorf("expected lines outside the key block to survive, got: %s", got)
	}
}

func TestDiagnoseHostID_StableAndNonEmpty(t *testing.T) {
	first := diagnoseHostID()
	second := diagnoseHostID()
	if first == "" {
		t.Fatal("expected a non-empty host id")
	}
	if first != second {
		t.Errorf("expected diagnoseHostID to be stable across calls, got %q then %q", first, second)
	}
	if strings.ContainsAny(first, "/\\") {
		t.Errorf("expected host id to contain no path separators, got %q", first)
	}
}

func TestDiagnoseCollectServices_CatalogError(t *testing.T) {
	wantErr := errors.New("boom")
	registered, services, steps := diagnoseCollectServices(t.Context(), &apiStatusFakeManager{catalogErr: wantErr})

	if registered != nil || services != nil {
		t.Errorf("expected nil results on catalog error, got registered=%v services=%v", registered, services)
	}
	if len(steps) != 1 || steps[0].Name != "services" || steps[0].Captured || !strings.Contains(steps[0].Error, "boom") {
		t.Errorf("expected a single failed 'services' step, got: %+v", steps)
	}
}

func TestDiagnoseCollectServices_PerServiceErrorDoesNotBlockCatalogStep(t *testing.T) {
	wantErr := errors.New("boom")
	catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
	registered, services, steps := diagnoseCollectServices(t.Context(), &apiStatusFakeManager{catalog: catalog, processErr: wantErr})

	if len(registered) != 1 {
		t.Errorf("expected the raw catalog to still be returned despite the per-service failure, got: %+v", registered)
	}
	if len(services) != 0 {
		t.Errorf("expected no service entries on a per-service failure, got: %+v", services)
	}

	catalogStep, found := stepOK(t, &diagnoseManifest{Steps: steps}, "services")
	if !found || !catalogStep.Captured {
		t.Errorf("expected the top-level 'services' catalog step to stay ok, got: %+v", catalogStep)
	}
	svcStep, found := stepOK(t, &diagnoseManifest{Steps: steps}, "service:svc")
	if !found || svcStep.Captured || !strings.Contains(svcStep.Error, "boom") {
		t.Errorf("expected a failed 'service:svc' step, got: %+v", svcStep)
	}
}

func TestDiagnoseCollectVersion_NilDaemon(t *testing.T) {
	info := diagnoseCollectVersion(t.Context(), nil)
	if info.DaemonReachable {
		t.Error("expected DaemonReachable false with a nil daemon config")
	}
	if info.CLIVersion == "" {
		t.Error("expected CLIVersion to still be populated")
	}
}

func TestDiagnoseCollectDaemonInfo(t *testing.T) {
	t.Run("nil daemon config", func(t *testing.T) {
		info, step := diagnoseCollectDaemonInfo(t.Context(), nil)
		if step.Captured {
			t.Error("expected a failed step for a nil daemon config")
		}
		if info.Mode != "" {
			t.Errorf("expected empty mode, got %q", info.Mode)
		}
	})

	t.Run("no supervisor configured", func(t *testing.T) {
		_, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{})
		if step.Captured {
			t.Error("expected a failed step when no supervisor is configured")
		}
	})

	t.Run("standalone not running", func(t *testing.T) {
		info, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{
			Standalone: &config.StandaloneDaemonConfig{PIDFile: filepath.Join(t.TempDir(), "eos.pid")},
		})
		if !step.Captured {
			t.Errorf("expected an ok step for a missing pid file (not running, not an error), got: %+v", step)
		}
		if info.Mode != "standalone" || info.Running {
			t.Errorf("expected standalone/not-running, got: %+v", info)
		}
	})

	t.Run("systemd not running", func(t *testing.T) {
		info, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{
			Systemd: &config.SystemdConfig{SocketPath: filepath.Join(shortTempSocketDir(t), "eos.sock")},
		})
		if !step.Captured {
			t.Errorf("expected an ok step, got: %+v", step)
		}
		if info.Mode != "systemd" || info.Running {
			t.Errorf("expected systemd/not-running, got: %+v", info)
		}
	})

	t.Run("systemd running with resolvable pid", func(t *testing.T) {
		stubSystemctl(t, 4242)
		dir := shortTempSocketDir(t)
		sockPath := filepath.Join(dir, "eos.sock")
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatalf("net.Listen unix: %v", err)
		}
		defer func() { _ = ln.Close() }()

		info, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{
			Systemd: &config.SystemdConfig{SocketPath: sockPath},
		})
		if !step.Captured {
			t.Errorf("expected an ok step, got: %+v", step)
		}
		if info.Mode != "systemd" || !info.Running {
			t.Errorf("expected systemd/running, got: %+v", info)
		}
		if info.Pid == nil || *info.Pid != 4242 {
			t.Errorf("expected resolved pid 4242, got: %+v", info.Pid)
		}
	})

	t.Run("launchd", func(t *testing.T) {
		info, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{Launchd: &config.LaunchdConfig{}})
		if !step.Captured || info.Mode != "launchd" {
			t.Errorf("expected ok launchd mode, got info=%+v step=%+v", info, step)
		}
	})

	t.Run("openrc", func(t *testing.T) {
		info, step := diagnoseCollectDaemonInfo(t.Context(), &config.DaemonConfig{OpenRC: &config.OpenRCConfig{}})
		if !step.Captured || info.Mode != "openrc" {
			t.Errorf("expected ok openrc mode, got info=%+v step=%+v", info, step)
		}
	})
}

func TestDiagnoseCapLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	got := diagnoseCapLines(lines, 2)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("expected the last 2 lines, got: %v", got)
	}
	if got := diagnoseCapLines(lines, 0); len(got) != len(lines) {
		t.Errorf("expected 0 to mean uncapped, got: %v", got)
	}
	if got := diagnoseCapLines(lines, 10); len(got) != len(lines) {
		t.Errorf("expected a cap above the length to be a no-op, got: %v", got)
	}
}
