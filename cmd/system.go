package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"eos/internal/buildinfo"
	"eos/internal/config"
	"eos/internal/process"
	"eos/internal/ui"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

func newSystemCmd() *cobra.Command {
	systemCmd := &cobra.Command{
		Use:   "system",
		Short: "Manage the eos system settings",
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "See active system information and configurations",
		Run: func(cmd *cobra.Command, args []string) {
			installDir, baseDir, config, err := createSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return
			}
			infoCmd(cmd, installDir, baseDir, *config)
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Apply new update if available",
		Run: func(cmd *cobra.Command, args []string) {
			installDir, _, systemConfig, err := createSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				return
			}
			includePre, err := cmd.Flags().GetBool("pre")
			if err != nil {
				systemCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return
			}
			updateCmd(cmd.Context(), cmd, buildinfo.GetVersionOnly(), installDir, systemConfig.Daemon, runtime.GOARCH, runtime.GOOS, includePre)
		},
	}
	updateCmd.Flags().Bool("pre", false, "includes pre-releases in update check")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Get version of system",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(buildinfo.Get())
		},
	}

	systemCmd.AddCommand(infoCmd)
	systemCmd.AddCommand(updateCmd)
	systemCmd.AddCommand(versionCmd)

	return systemCmd
}

func infoCmd(cmd *cobra.Command, installDir string, baseDir string, config config.SystemConfig) {
	cmd.Println()
	cmd.Printf("%s\n\n", ui.TextBold.Render("System Config"))
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("install dir:"), installDir)
	cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("base dir:"), baseDir)
	cmd.Printf("%s\n\n", ui.TextBold.Render("Daemon"))
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("pid file:"), config.Daemon.PIDFile)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket:"), config.Daemon.SocketPath)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket timeout:"), config.Daemon.SocketTimeout)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log dir:"), config.Daemon.LogDir)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log file:"), config.Daemon.LogFileName)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("max files:"), config.Daemon.MaxFiles)
	cmd.Printf("  %s %d\n\n", ui.TextMuted.Render("size limit:"), config.Daemon.FileSizeLimit)
	cmd.Printf("%s\n\n", ui.TextBold.Render("Health Check"))
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("max restarts:"), config.Health.MaxRestart)
	cmd.Printf("  %s %v\n", ui.TextMuted.Render("timeout enabled:"), config.Health.Timeout.Enable)
	if config.Health.Timeout.Enable {
		cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit)
	} else {
		cmd.Printf("  %s %s %s\n\n", ui.TextMuted.Render("timeout limit:"), config.Health.Timeout.Limit, ui.TextMuted.Render("(not active)"))
	}
	cmd.Printf("%s\n\n", ui.TextBold.Render("Shutdown"))
	cmd.Printf("  %s %v\n", ui.TextMuted.Render("grace period:"), config.Shutdown.GracePeriod)
}

func updateCmd(ctx context.Context, cmd *cobra.Command, version string, installDir string, daemonConfig config.DaemonConfig, userArch string, userOS string, includePre bool) {
	binaryPath := filepath.Join(installDir, "eos")

	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "checking for updates...")

	fileInfo, err := os.Stat(installDir)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", installDir))
		return
	}

	if !fileInfo.IsDir() {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", installDir))
		return
	}

	if version == "dev" {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "updating not supported for dev builds")
		return
	}

	if !strings.HasPrefix(version, "v") {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid version tag, must start with 'v'")
		return
	}

	if !semver.IsValid(version) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid semantic version")
		return
	}

	release, err := fetchLatestRelease(ctx, includePre)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("fetching latest release: %v", err))
		return
	}

	latestVersion, latestAsset, err := checkForUpdates(release, version, userArch, userOS)

	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking for updates: %v", err))
		return
	}
	if latestVersion == "" {
		cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), "already on the latest version", ui.TextMuted.Render(fmt.Sprintf("(%s)", version)))
		return
	}
	if latestAsset == nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("no compatible asset found for %s-%s", userOS, userArch))
		return
	}

	cmd.Printf("%s %s → %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(version), ui.TextBold.Render(latestVersion))
	cmd.Printf("  %s ", ui.TextMuted.Render("upgrade? (y/n):"))

	reader := bufio.NewReader(cmd.InOrStdin())
	response, err := reader.ReadString('\n')

	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading input: %v", err))
		return
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "update canceled")
		return
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), fmt.Sprintf("downloading eos %s for %s-%s...", latestVersion, userOS, userArch))
	binary, tempDir, err := handleDownloadBinary(ctx, latestAsset)

	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("downloading binary: %v", err))
		return
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "validating checksums...")
	err = validateDigest(latestAsset, binary)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checksum validation failed: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "checksums match")

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, timestamp)
	if err := copyFile(binaryPath, backupPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("backing up current binary: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return
	}

	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("info"), "backup created at", ui.TextMuted.Render(backupPath))

	if err := replaceBinary(binary.Name(), binaryPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("installing new binary: %v", err))
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
		}
		return
	}
	if err := os.Chmod(binaryPath, 0755); err != nil { // #nosec G302 -- executable binary needs to be runnable by all users
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("setting permissions: %v", err))
		return
	}

	cmd.Printf("  %s ", ui.TextMuted.Render("restart daemon? (y/n):"))

	response, readErr := reader.ReadString('\n')
	if readErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading input: %v", readErr))
		return
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "manual daemon restart required")
		cmd.Printf("\n%s %s %s\n\n", ui.LabelSuccess.Render("success"), "eos updated to", ui.TextBold.Render(latestVersion))
		return
	}

	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
	killed, killErr := process.StopDaemon(daemonConfig)

	if killErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
		return
	}
	if !killed {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
		return
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")

	if err := forkDaemon(ctx); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		return
	}
	cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon started in background")

	if err := os.RemoveAll(tempDir); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, err))
	}

	cmd.Printf("\n%s %s %s\n\n", ui.LabelSuccess.Render("success"), "eos updated to", ui.TextBold.Render(latestVersion))
}

type Asset struct {
	Name               string `json:"name"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

func fetchLatestRelease(ctx context.Context, includePre bool) (*Release, error) {
	url := "https://api.github.com/repos/Elysium-Labs-EU/eos/releases/latest"
	if includePre {
		url = "https://api.github.com/repos/Elysium-Labs-EU/eos/releases"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request building failed: %w", err)
	}

	// #nosec G704
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing response body: %w", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	if includePre {
		var releases []Release
		if err = json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		if len(releases) == 0 {
			return nil, fmt.Errorf("no releases found")
		}
		return &releases[0], nil
	}

	var release Release
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &release, nil
}

func checkForUpdates(release *Release, current string, arch string, os string) (latestVersion string, asset *Asset, err error) {
	latest := release.TagName

	if semver.Compare(current, latest) >= 0 {
		return "", nil, nil
	}

	var usuableAsset *Asset
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, arch) && strings.Contains(asset.Name, os) {
			usuableAsset = &asset
		}
	}

	if usuableAsset == nil {
		return "", nil, fmt.Errorf("no usable asset found")
	}

	return latest, usuableAsset, nil
}

func handleDownloadBinary(ctx context.Context, latestAsset *Asset) (_ *os.File, tempDir string, err error) {
	parsedURL, err := url.Parse(latestAsset.BrowserDownloadURL)
	if err != nil || parsedURL.Host != "github.com" {
		return nil, "", fmt.Errorf("invalid URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAsset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("request building failed: %w", err)
	}

	resp, err := httpClient.Do(req) // #nosec G704 -- URL is constructed from hardcoded GitHub API base, not user input
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing response body: %w", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	cleanUpRequiredOnError := true
	tempDir, err = os.MkdirTemp("", "tempDownloadDir")
	if err != nil {
		return nil, "", fmt.Errorf("unable to create temporary download directory for downloading binary: %w", err)
	}
	defer func() {
		if cleanUpRequiredOnError {
			if cleanUpErr := os.RemoveAll(tempDir); cleanUpErr != nil {
				if err != nil {
					err = fmt.Errorf("%w; cleanup also failed: %w", err, cleanUpErr)
				} else {
					err = fmt.Errorf("cleaning up temporary directory: %w", cleanUpErr)
				}
			}
		}
	}()

	file, err := os.Create(filepath.Clean(filepath.Join(tempDir, latestAsset.Name)))
	if err != nil {
		return nil, "", fmt.Errorf("errored during creating file for downloading binary: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("errored during copying contents of fetched binary: %w", err)
	}

	if resp.ContentLength != -1 && written != resp.ContentLength {
		return nil, "", fmt.Errorf("received file doesn't match expected size")
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, "", fmt.Errorf("failed to reset seeker on the file: %w", err)
	}

	cleanUpRequiredOnError = false
	return file, tempDir, nil
}

func validateDigest(latestAsset *Asset, binary *os.File) error {
	_, err := binary.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to reset seeker on the file: %w", err)
	}

	receivedChecksum := strings.TrimPrefix(latestAsset.Digest, "sha256:")

	hasher := sha256.New()

	if _, err := io.Copy(hasher, binary); err != nil {
		return fmt.Errorf("failed to hash binary: %w", err)
	}
	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if receivedChecksum != calculatedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", receivedChecksum, calculatedChecksum)
	}

	return nil
}

func copyFile(src string, dst string) (err error) {
	source, err := os.Open(filepath.Clean(src)) // #nosec G703 -- src is constructed internally, not from user input
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if sourceCloseErr := source.Close(); sourceCloseErr != nil && err == nil {
			err = fmt.Errorf("errored closing the source file: %w", sourceCloseErr)
		}
	}()

	destination, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if destinationCloseErr := destination.Close(); destinationCloseErr != nil && err == nil {
			err = fmt.Errorf("errored closing the destination file: %w", destinationCloseErr)
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(dst); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = fmt.Errorf("failed to remove partial destination file: %w", removeErr)
			}
		}
	}()

	return nil
}

func replaceBinary(src string, dst string) (err error) {
	source, err := os.Open(filepath.Clean(src)) // #nosec G703 -- src is constructed internally, not from user input
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("errored closing the source file: %w", closeErr)
		}
	}()

	tmpDst := dst + ".tmp"

	destination, err := os.Create(filepath.Clean(tmpDst))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = destination.Close()
			_ = os.Remove(tmpDst)
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	if err = destination.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err = os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	return nil
}
