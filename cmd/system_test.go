package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/buildinfo"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"github.com/spf13/cobra"
)

// slowReader wraps an io.Reader and returns one byte per Read call.
// Required because PromptConfirm creates a new bufio.Reader on each call —
// bufio pre-buffers the whole underlying reader on first fill, leaving nothing
// for the next prompt. One-byte reads prevent that lookahead.
type slowReader struct{ r io.Reader }

func (s *slowReader) Read(p []byte) (int, error) {
	return s.r.Read(p[:1])
}

func makeTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c := &cobra.Command{}
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	return c, &out, &errOut
}

func setStdin(c *cobra.Command, input string) {
	c.SetIn(&slowReader{strings.NewReader(input)})
}

func fakeDetectRuntime(runtime string) func() (string, error) {
	return func() (string, error) { return runtime, nil }
}

func fakeDetectRuntimeErr(err error) func() (string, error) {
	return func() (string, error) { return "", err }
}

func recordingRunCmd(t *testing.T, calls *[]string) runCmdFn {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		return []byte("ok"), nil
	}
}

func noopRunCmd(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("ok"), nil
}

func TestStartupCmdNonSystemdRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	startupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos.service", false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestStartupCmdRuntimeDetectionError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	startupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos.service", false,
		fakeDetectRuntimeErr(fmt.Errorf("no /proc")), noopRunCmd)

	if !strings.Contains(errBuf.String(), "getting system command") {
		t.Errorf("expected runtime error in stderr, got: %s", errBuf.String())
	}
}

func TestStartupCmdDeclineUnitFile(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	startupCmd(t.Context(), c, "/usr/local/bin", &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls when user declines, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "unit file creation canceled") {
		t.Errorf("expected cancelation message, got: %s", outBuf.String())
	}
}

func TestStartupCmdWritesUnitFileAndEnablesWithoutRestart(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unit file creation, decline restart
	setStdin(c, "y\nn\n")

	var calls []string
	startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	unitFilePath := filepath.Join(tempDir, "eos.service")
	if _, err := os.Stat(unitFilePath); os.IsNotExist(err) {
		t.Error("expected unit file to be written")
	}

	want := []string{"systemctl daemon-reload", "systemctl enable eos"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "system unit enabled, eos will start on boot") {
		t.Errorf("expected 'system unit enabled' message, got: %s", outBuf.String())
	}
}

func TestStartupCmdFullRestartPath(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	// confirm unit file, confirm restart
	setStdin(c, "y\ny\n")

	var calls []string
	startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	want := []string{"systemctl daemon-reload", "systemctl enable eos", "systemctl start eos"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}
}

func TestUnstartupCmdNonSystemdRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	unstartupCmd(t.Context(), c, config.SystemdConfig{}, false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestUnstartupCmdDeclineConfirmation(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	unstartupCmd(t.Context(), c, config.SystemdConfig{}, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls when declined, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "canceled") {
		t.Errorf("expected 'canceled' message, got: %s", outBuf.String())
	}
}

func TestUnstartupCmdRemovesUnitAndReloads(t *testing.T) {
	tempDir := t.TempDir()
	unitFile := filepath.Join(tempDir, "eos.service")
	if err := os.WriteFile(unitFile, []byte("[Unit]"), 0644); err != nil {
		t.Fatal(err)
	}

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	var calls []string
	unstartupCmd(t.Context(), c, config.SystemdConfig{
		SystemdTargetDir:      tempDir + "/",
		SystemdTargetFileName: "eos.service",
	}, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	if _, err := os.Stat(unitFile); !os.IsNotExist(err) {
		t.Error("expected unit file to be removed")
	}

	want := []string{"systemctl stop eos", "systemctl disable eos", "systemctl daemon-reload"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "system unit startup removed") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

func TestSystemInfoCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"system", "info"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing info test - add should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "System Config") {
		t.Errorf("expected the output to contain 'System Config' header, got: %s", output)
	}
}

func TestSystemUpdateCommand(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"system", "update"})

	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing update test - should not return an error, got: %v\n", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "dev") {
		t.Errorf("expected the output to contain 'dev', got: %s", output)
	}
}

// func TestCopyFile_TextFileBusy(t *testing.T) {
// 	if runtime.GOOS != "linux" {
// 		t.Skip("text file busy error is Linux-specific")
// 	}

// 	tempDir := t.TempDir()

// 	// Create a minimal executable binary (a simple sleep script won't work;
// 	// we need an actual ELF binary). We'll compile a tiny Go program.
// 	srcPath := filepath.Join(tempDir, "main.go")
// 	err := os.WriteFile(srcPath, []byte(`package main
// import "time"
// func main() { time.Sleep(30 * time.Second) }
// `), 0644)
// 	if err != nil {
// 		t.Fatalf("failed to write source file: %v", err)
// 	}

// 	binaryPath := filepath.Join(tempDir, "eos")
// 	buildCmd := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, srcPath)
// 	if out, outputErr := buildCmd.CombinedOutput(); outputErr != nil {
// 		t.Fatalf("failed to compile test binary: %v\n%s", outputErr, out)
// 	}

// 	// Start the binary so the OS holds it as "text busy"
// 	ctx, cancel := context.WithCancel(t.Context())
// 	defer cancel()

// 	proc := exec.CommandContext(ctx, binaryPath)
// 	if startErr := proc.Start(); startErr != nil {
// 		t.Fatalf("failed to start test binary: %v", startErr)
// 	}
// 	defer func() {
// 		cancel()
// 		_ = proc.Wait()
// 	}()

// 	time.Sleep(100 * time.Millisecond)

// 	replacementPath := filepath.Join(tempDir, "eos_new")
// 	err = os.WriteFile(replacementPath, []byte("new binary content"), 0755)
// 	if err != nil {
// 		t.Fatalf("failed to create replacement file: %v", err)
// 	}

// 	err = copyFile(replacementPath, binaryPath)
// 	if err == nil {
// 		t.Fatal("expected 'text file busy' error, got nil")
// 	}

// 	if !strings.Contains(err.Error(), "text file busy") {
// 		t.Errorf("expected 'text file busy' in error message, got: %v", err)
// 	}
// }

func TestSystemUpdateWithInvalidVersionCommand(t *testing.T) {
	buildinfo.Version = "invalid-version"
	defer func() { buildinfo.Version = "dev" }()

	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"system", "update"})

	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing update test - should not return an error, got: %v\n", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "invalid version tag, must start with 'v") {
		t.Errorf("expected the output to contain 'invalid version tag, must start with 'v', got: %s", output)
	}
}

func TestSystemUpdateWithInvalidOSArchCombinationCommand(t *testing.T) {
	buildinfo.Version = "v0.0.1"
	defer func() { buildinfo.Version = "dev" }()

	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	installDir, baseDir, systemConfig, err := newSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - newSystemConfig should not return an error: %v\n", err)
	}

	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd)
	if err != nil {
		t.Fatalf("preparing update test - newDaemonController should not return an error: %v\n", err)
	}

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{
			TagName: "v99.0.0",
			Assets:  []Asset{{Name: "eos-linux-arm64"}},
		}, nil
	}

	updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, ctrl, "arm64", "darwin", false, fakeFetchRelease, handleDownloadBinary)

	output := errBuf.String()

	if !strings.Contains(output, "no compatible asset found") {
		t.Errorf("expected the output to contain 'no compatible asset found', got: %s", output)
	}
}

func TestSystemUpdateWithLowerVersionCommand(t *testing.T) {
	buildinfo.Version = "v0.0.1"
	defer func() { buildinfo.Version = "dev" }()

	cmd, outBuf, _, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error: %v\n", err)
	}

	installDir, baseDir, systemConfig, err := newSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - newSystemConfig should not return an error: %v\n", err)
	}

	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd)
	if err != nil {
		t.Fatalf("preparing update test - newDaemonController should not return an error: %v\n", err)
	}

	binaryContent := []byte("fake binary")
	sum := sha256.Sum256(binaryContent)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{
			TagName: "v99.0.0",
			Assets: []Asset{
				{
					Name:               "eos-linux-arm64",
					Digest:             digest,
					BrowserDownloadURL: "https://codeberg.org/fake/download",
				},
			},
		}, nil
	}

	fakeDownloadBinary := func(_ context.Context, asset *Asset) (*os.File, string, error) {
		dir := t.TempDir()
		f, createErr := os.CreateTemp(dir, asset.Name)
		if createErr != nil {
			return nil, "", createErr
		}
		if _, writeErr := f.Write(binaryContent); writeErr != nil {
			_ = f.Close()
			return nil, "", writeErr
		}
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			_ = f.Close()
			return nil, "", seekErr
		}
		return f, dir, nil
	}

	cmd.SetIn(strings.NewReader("y\ny\n"))

	updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, ctrl, "arm64", "linux", false, fakeFetchRelease, fakeDownloadBinary)

	output := outBuf.String()

	if !strings.Contains(output, "info checksums match") {
		t.Errorf("expected the output to contain 'info checksums match', got: %s", output)
	}
}

func TestSystemUpdateCheckWritableFailed(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	dir := t.TempDir()
	err := os.Chmod(dir, 0555)
	if err != nil {
		t.Fatalf("errored during test setup")
	}

	testCmd := &cobra.Command{}

	err = checkWritable(testCmd, dir)
	if err == nil {
		t.Fatalf("expected permission issues, got: %v", err)
	}
}

func TestSystemUpdateCopyFile(t *testing.T) {
	dir := t.TempDir()

	src, err := os.CreateTemp(dir, "srcTest")
	if err != nil {
		t.Fatal("creating file srcTest")
	}

	testContent := "Hello World"
	err = os.WriteFile(src.Name(), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("writing file srcTest, got: %v", err)
	}

	dst, err := os.CreateTemp(dir, "dstTest")
	if err != nil {
		t.Fatalf("creating file dstTest, got: %v", err)
	}

	err = copyFile(src.Name(), dst.Name())
	if err != nil {
		t.Fatalf("copyFile errored, got: %v", err)
	}

	content, err := os.ReadFile(dst.Name())
	if err != nil {
		t.Fatalf("read destination file, got: %v", err)
	}
	if string(content) != testContent {
		t.Fatalf("expected to read same content on destination as source, got: %s", string(content))
	}
}

func TestSystemUpdateCreateDestinationFile(t *testing.T) {
	dir := t.TempDir()

	testFile := filepath.Join(dir, "dstTest")
	err := createDestinationFile(testFile)
	if err != nil {
		t.Fatalf("expected create destination file to not error, got: %v", err)
	}
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatalf("expected file to exist, but it does not")
	}
}

func TestSystemVersionCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"system", "version"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing version test - add should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "dev") {
		t.Errorf("expected the output to contain 'dev', got: %s", output)
	}
}

// func TestReplaceBinary(t *testing.T) {}

func TestSupportedPlatformsMatchCheckForUpdates(t *testing.T) {
	for _, platform := range supportedPlatforms {
		parts := strings.SplitN(platform, "-", 2)
		if len(parts) != 2 {
			t.Errorf("supportedPlatforms entry %q is not in os-arch format", platform)
			continue
		}
		goos, goarch := parts[0], parts[1]

		release := &Release{
			TagName: "v99.0.0",
			Assets:  []Asset{{Name: "eos-" + goos + "-" + goarch}},
		}
		result, err := checkForUpdates(release, "v0.0.1", goarch, goos)
		if err != nil {
			t.Errorf("supportedPlatforms entry %q not matched by checkForUpdates: %v", platform, err)
		}
		if result.Asset == nil {
			t.Errorf("supportedPlatforms entry %q: checkForUpdates returned nil asset", platform)
		}
	}
}
