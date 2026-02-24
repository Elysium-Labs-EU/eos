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
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

func newSystemCmd() *cobra.Command {
	systemCmd := &cobra.Command{
		Use:   "system",
		Short: "Manage the eos system settings",
	}

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "See active system config",
		Run: func(cmd *cobra.Command, args []string) {
			installDir, baseDir, config, err := createSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("Error getting system configuration: %v\n", err)
				os.Exit(1)
			}
			configCmd(cmd, installDir, baseDir, *config)
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Apply new update if available",
		Run: func(cmd *cobra.Command, args []string) {
			installDir, _, _, err := createSystemConfig()
			if err != nil {
				systemCmd.PrintErrf("Error getting system configuration: %v\n", err)
				os.Exit(1)
			}
			updateCmd(cmd, buildinfo.GetVersionOnly(), installDir)
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Get version of system",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(buildinfo.Get())
		},
	}

	systemCmd.AddCommand(configCmd)
	systemCmd.AddCommand(updateCmd)
	systemCmd.AddCommand(versionCmd)

	return systemCmd
}

func configCmd(cmd *cobra.Command, installDir string, baseDir string, config config.SystemConfig) {
	cmd.Println("")
	cmd.Println("System Config")
	cmd.Println("")
	cmd.Printf("Install directory: %s\n", installDir)
	cmd.Printf("Base directory: %s\n", baseDir)
	cmd.Println("")
	cmd.Println("# Daemon configuration")
	cmd.Printf("PID File: %s\n", config.Daemon.PIDFile)
	cmd.Printf("Socket Path: %s\n", config.Daemon.SocketPath)
	cmd.Printf("Log Directory: %s\n", config.Daemon.LogDir)
	cmd.Printf("Log Filename: %s\n", config.Daemon.LogFileName)
	cmd.Printf("Log maximum files: %d\n", config.Daemon.MaxFiles)
	cmd.Printf("Log filesize limit: %d\n", config.Daemon.FileSizeLimit)
	cmd.Println("")
	cmd.Println("# Process health check configuration")
	cmd.Printf("Max number of restarts: %d\n", config.Health.MaxRestart)
	cmd.Printf("Check process on timeout: %v\n", config.Health.Timeout.Enable)
	if config.Health.Timeout.Enable {
		cmd.Printf("Process timeout limit: %s\n", config.Health.Timeout.Limit)
	} else {
		cmd.Printf("Process timeout limit: %s (not active)\n", config.Health.Timeout.Limit)
	}
}

func updateCmd(cmd *cobra.Command, version string, installDir string) {
	userArch := runtime.GOARCH
	userOS := runtime.GOOS
	binaryPath := filepath.Join(installDir, "eos")
	cmd.Println("Checking for updates...")

	fileInfo, err := os.Stat(installDir)
	if err != nil {
		cmd.Printf("Directory %q for updates is not accessible, please check.\n", installDir)
		return
	}

	if !fileInfo.IsDir() {
		cmd.Printf("Directory %q for updates is not accessible, please check.\n", installDir)
		return
	}

	if version == "dev" {
		cmd.Println("Updating not supported, your version is detected to be 'dev'.")
		return
	}

	if !strings.HasPrefix(version, "v") {
		cmd.Println("Current version tag name has an invalid pattern, it doesn't start with a 'v'.")
		return
	}

	if !semver.IsValid(version) {
		cmd.Println("Invalid semantic version.")
		return
	}

	release, err := fetchLatestRelease(cmd.Context())
	if err != nil {
		cmd.PrintErrf("Unable to retrieve latest release, got: %v", err)
		return
	}

	latestVersion, latestAsset := checkForUpdates(release, version, userArch, userOS)

	if latestVersion == "" {
		cmd.Printf("Current version (%s) is the latest version.\n", version)
		return
	}
	if latestAsset == nil {
		cmd.Printf("No usuable asset found for the latest version (%s).\n", version)
		return
	}

	cmd.Printf("A newer version has been found (%s). Current version (%s).\n", latestVersion, version)
	cmd.Printf("Would you like to upgrade? (y/n): ")

	reader := bufio.NewReader(cmd.InOrStdin())
	response, err := reader.ReadString('\n')

	if err != nil {
		cmd.Printf("Error reading input: %v\n", err)
		return
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		cmd.Println("Canceled update")
		return
	}

	cmd.Printf("Downloading eos %s for %s-%s...\n", latestVersion, userOS, userArch)
	binary, tempDir, err := handleDownloadBinary(cmd.Context(), latestAsset)

	if err != nil {
		cmd.PrintErrf("Downloading the binary failed, got: %v", err)
		return
	}

	cmd.Println("Validating checksums...")
	err = validateDigest(latestAsset, binary)
	if err != nil {
		cmd.PrintErrf("Error during validating checksums: %v", err)
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("Cleaning up the temporary directory at %s failed, %v - manual clean-up advised", tempDir, cleanupErr)
		}
		return
	}

	cmd.Println("Checksums match. Proceeding...")

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, timestamp)
	if err := copyFile(binaryPath, backupPath); err != nil {
		cmd.PrintErrf("Failed to backup current binary: %v", err)
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("Cleaning up the temporary directory at %s failed, %v - manual clean-up advised", tempDir, cleanupErr)
		}
		return
	}

	cmd.Printf("Current binary backed up to %s\n", backupPath)

	if err := copyFile(binary.Name(), binaryPath); err != nil {
		cmd.PrintErrf("Failed to install new binary: %v", err)
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			cmd.PrintErrf("Cleaning up the temporary directory at %s failed, %v - manual clean-up advised", tempDir, cleanupErr)
		}
		return
	}
	if err := os.Chmod(binaryPath, 0755); err != nil { // #nosec G302 -- executable binary needs to be runnable by all users
		cmd.PrintErrf("Failed to set permissions: %v", err)
		return
	}

	cmd.Println("New binary installed successfully")

	if err := os.RemoveAll(tempDir); err != nil {
		cmd.PrintErrf("Cleaning up the temporary directory at %s failed, %v - manual clean-up advised", tempDir, err)
	}
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

func fetchLatestRelease(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Elysium-Labs-EU/eos/releases/latest", nil)
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

	var release Release
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &release, nil
}

func checkForUpdates(release *Release, current string, arch string, os string) (string, *Asset) {
	latest := release.TagName

	if semver.Compare(current, latest) >= 0 {
		return "", nil
	}

	var usuableAsset *Asset
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, arch) && strings.Contains(asset.Name, os) {
			usuableAsset = &asset
		}
	}

	if usuableAsset == nil {
		return "", nil
	}

	return latest, usuableAsset
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
