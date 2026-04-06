package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/spf13/cobra"
)

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

	installDir, _, systemConfig, err := createSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - createSystemConfig should not return an error: %v\n", err)
	}

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{
			TagName: "v99.0.0",
			Assets:  []Asset{{Name: "eos-linux-arm64"}},
		}, nil
	}

	updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, systemConfig.Daemon, "arm64", "darwin", false, fakeFetchRelease, handleDownloadBinary)

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

	installDir, _, systemConfig, err := createSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - createSystemConfig should not return an error: %v\n", err)
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
					BrowserDownloadURL: "https://github.com/fake/download",
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

	updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, systemConfig.Daemon, "arm64", "linux", false, fakeFetchRelease, fakeDownloadBinary)

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
