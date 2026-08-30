package cmd

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

// sysRunUpdate backs the "system update" subcommand's RunE.
func sysRunUpdate(cmd *cobra.Command, systemCmd *cobra.Command, ctrl DaemonController) error {
	installDir, _, _, _, err := newSystemConfig()
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
		return helpers.ErrCommandFailed
	}
	includePre, err := cmd.Flags().GetBool("pre")
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
		return helpers.ErrCommandFailed
	}

	version, userArch, userOS := sysUpdateEnvOverrides(os.Getenv)
	return updateCmd(cmd.Context(), cmd, &updateCmdParams{
		Version:    version,
		InstallDir: installDir,
		Ctrl:       ctrl,
		UserArch:   userArch,
		UserOS:     userOS,
		IncludePre: includePre,
	}, fetchLatestRelease, handleDownloadBinary, fetchChecksumForBinary)
}

// sysUpdateEnvOverrides resolves the version/arch/OS the update check runs
// against, letting tests override each independently of the real build and
// host. Pure — reads only the given env lookup function.
func sysUpdateEnvOverrides(getenv func(string) string) (version, userArch, userOS string) {
	version = buildinfo.GetVersionOnly()
	if override := getenv("EOS_VERSION"); override != "" {
		version = override
	}
	userArch = runtime.GOARCH
	if override := getenv("USER_ARCH"); override != "" {
		userArch = override
	}
	userOS = runtime.GOOS
	if override := getenv("USER_OS"); override != "" {
		userOS = override
	}
	return version, userArch, userOS
}

// updateCmdParams bundles the plain-value parameters updateCmd needs to
// check for, download, and install a new eos release.
type updateCmdParams struct {
	Version    string
	InstallDir string
	Ctrl       DaemonController
	UserArch   string
	UserOS     string
	IncludePre bool
}

func updateCmd(ctx context.Context, cmd *cobra.Command, p *updateCmdParams, fetchRelease func(context.Context, bool) (*Release, error), downloadBinary func(context.Context, *Asset) (*os.File, string, error), getChecksum func(context.Context, *Asset, string) (string, error)) error {
	binaryPath := filepath.Join(p.InstallDir, "eos")

	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "checking for updates...")

	if err := validateUpdatePreconditions(cmd, p.InstallDir, p.Version); err != nil {
		return err
	}

	result, proceed, err := resolveUpdateTarget(ctx, cmd, fetchRelease, p.Version, p.UserArch, p.UserOS, p.IncludePre)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	cmd.Printf("%s %s to %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(p.Version), ui.TextBold.Render(result.LatestVersion))
	if !helpers.PromptConfirm(cmd, "upgrade? (y/n):") {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "update canceled")
		return nil
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), fmt.Sprintf("downloading eos %s for %s-%s...", result.LatestVersion, p.UserOS, p.UserArch))
	binary, tempDir, err := downloadAndVerifyBinary(ctx, cmd, result, downloadBinary, getChecksum)
	if err != nil {
		return err
	}

	if err := installUpdatedBinary(cmd, binary, binaryPath, tempDir); err != nil {
		return err
	}

	refreshInstalledCompletions(ctx, cmd, binaryPath)

	return restartDaemonAfterUpdate(ctx, cmd, p.Ctrl, tempDir, result.LatestVersion)
}

// supportedPlatforms lists the OS-arch combinations for which eos releases are published.
// Keep this in sync with the build pipeline.
var supportedPlatforms = []string{
	"linux-amd64",
	"linux-arm64",
}

// updateUserAgent is sent on every request to the GitHub release API and asset
// downloads. The GitHub REST API rejects requests without a User-Agent with a
// 403
const updateUserAgent = "eos-updater"

// releaseSigningPublicKeyPEM is the ECDSA P-256 public key (SubjectPublicKeyInfo,
// PEM) used to verify the detached signature over each release's
// sha256sums.txt. The matching private key lives only as the
// RELEASE_SIGNING_KEY secret in GitHub Actions and is used by
// .github/workflows/release.yml (or build-and-release.yml) to sign at
// release time — it is never checked into this repo.
const releaseSigningPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEByucQHF5ASSSrPSu6Gb5fvAuWdMw
BNAGlV57YMjkCdpcq8HHRXYXHXqy3cvfIzHYE2UHfftsk83lrSXPkxMyZg==
-----END PUBLIC KEY-----
`

// requireReleaseSignature gates whether a release with no sha256sums.txt.sig
// asset is refused outright rather than merely warned about. Enforced now
// that RELEASE_SIGNING_KEY is provisioned in GitHub Actions and signed
// releases are published — an unsigned or signature-stripped release can no
// longer be installed silently. Keep install.sh's REQUIRE_RELEASE_SIGNATURE
// in sync with this value.
const requireReleaseSignature = true

// parseReleaseSigningPublicKey decodes the embedded release signing public
// key. Pure — no I/O.
func parseReleaseSigningPublicKey() (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(releaseSigningPublicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decoding embedded release signing public key: no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing embedded release signing public key: %w", err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded release signing public key is %T, want ECDSA", pub)
	}
	return ecdsaPub, nil
}

// downloadAndVerifyBinary downloads the release asset and verifies its checksum,
// cleaning up the temp dir on any verification failure.
func downloadAndVerifyBinary(ctx context.Context, cmd *cobra.Command, result UpdateResult, downloadBinary func(context.Context, *Asset) (*os.File, string, error), getChecksum func(context.Context, *Asset, string) (string, error)) (*os.File, string, error) {
	binary, tempDir, err := downloadBinary(ctx, result.Asset)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("downloading binary: %v", err))
		return nil, "", helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "validating checksums...")
	expectedChecksum, err := getChecksum(ctx, result.ChecksumsAsset, result.Asset.Name)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("fetching checksums: %v", err))
		cleanupTempDir(cmd, tempDir)
		return nil, "", helpers.ErrCommandFailed
	}

	if err := validateDigest(expectedChecksum, binary); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checksum validation failed: %v", err))
		cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(cmdnames.HintSystemUpdate) + ui.TextMuted.Render(" to retry the update") + "\n")
		cleanupTempDir(cmd, tempDir)
		return nil, "", helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "checksums match")

	if err := verifyReleaseSignature(ctx, cmd, result); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), err.Error())
		cleanupTempDir(cmd, tempDir)
		return nil, "", helpers.ErrCommandFailed
	}

	return binary, tempDir, nil
}

// verifyReleaseSignature fetches result's sha256sums.txt and
// sha256sums.txt.sig and verifies the signature, writing a status line to
// cmd either way.
//
// A release with no signature asset is a hard error while
// requireReleaseSignature is true (see its doc comment); the warning path
// below is retained for the rollout window before a signing key existed,
// since sha256 checksum verification (downloadAndVerifyBinary) already runs
// independently of this. A signature asset that fails to verify is always a
// hard error — that's a stronger integrity signal than "no signature was
// ever published", so it's never soft-failed.
func verifyReleaseSignature(ctx context.Context, cmd *cobra.Command, result UpdateResult) error {
	checksumsData, err := fetchChecksumsFile(ctx, result.ChecksumsAsset)
	if err != nil {
		return err
	}

	if result.SignatureAsset == nil {
		if requireReleaseSignature {
			return fmt.Errorf("release %s has no sha256sums.txt.sig", result.LatestVersion)
		}
		cmd.Printf(fmtLabelMsgLn, ui.LabelWarning.Render("warning"), fmt.Sprintf("release %s has no signature (sha256sums.txt.sig) — checksum-only integrity", result.LatestVersion))
		return nil
	}

	resp, err := fetchAssetResponse(ctx, result.SignatureAsset)
	if err != nil {
		return fmt.Errorf("fetching signature: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response, close error not actionable
	sigData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading signature: %w", err)
	}

	if verifyErr := verifyChecksumsSignature(checksumsData, sigData); verifyErr != nil {
		return fmt.Errorf("signature verification failed for %s: %w — refusing to install", result.LatestVersion, verifyErr)
	}
	cmd.Printf(fmtLabelMsgLn, ui.LabelSuccess.Render("success"), "signature verified")
	return nil
}

// fetchChecksumsFile downloads the full raw bytes of the release's
// sha256sums.txt, for signature verification over the whole file (as opposed
// to fetchChecksumForBinary, which only extracts one binary's hash line).
func fetchChecksumsFile(ctx context.Context, checksumsAsset *Asset) ([]byte, error) {
	if checksumsAsset == nil {
		return nil, fmt.Errorf("no sha256sums.txt asset in release")
	}
	resp, err := fetchAssetResponse(ctx, checksumsAsset)
	if err != nil {
		return nil, fmt.Errorf("fetching sha256sums.txt: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response, close error not actionable

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sha256sums.txt: %w", err)
	}
	return data, nil
}

// verifyChecksumsSignature checks sig against checksumsData using the
// embedded release signing public key. Pure — no I/O.
func verifyChecksumsSignature(checksumsData, sig []byte) error {
	pub, err := parseReleaseSigningPublicKey()
	if err != nil {
		return err
	}
	if err := verifySignature(pub, checksumsData, sig); err != nil {
		return fmt.Errorf("signature does not match sha256sums.txt")
	}
	return nil
}

// verifySignature checks sig — an ASN.1 DER ECDSA signature, as produced by
// `openssl dgst -sha256 -sign` — against the SHA-256 digest of data, using
// pub. Pure — no I/O.
func verifySignature(pub *ecdsa.PublicKey, data, sig []byte) error {
	digest := sha256.Sum256(data)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return fmt.Errorf("signature does not match")
	}
	return nil
}

func validateDigest(expectedChecksum string, binary *os.File) error {
	if _, err := binary.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to reset seeker on the file: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, binary); err != nil {
		return fmt.Errorf("failed to hash binary: %w", err)
	}
	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if expectedChecksum != calculatedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculatedChecksum)
	}

	return nil
}

// restartDaemonAfterUpdate optionally restarts the daemon on the new binary,
// removes the temp dir, and prints the final success summary.
func restartDaemonAfterUpdate(ctx context.Context, cmd *cobra.Command, ctrl DaemonController, tempDir, latestVersion string) error {
	if !ctrl.IsRunning(ctx) {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(msgDaemonWasNotRunning))
		cmd.Printf("\n%s %s %s\n\n", ui.LabelSuccess.Render("success"), msgEosUpdatedTo, ui.TextBold.Render(latestVersion))
		return nil
	}

	if !helpers.PromptConfirm(cmd, "restart daemon? (y/n):") {
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), "manual daemon restart required")
		cmd.Printf("\n%s %s %s\n\n", ui.LabelSuccess.Render("success"), msgEosUpdatedTo, ui.TextBold.Render(latestVersion))
		return nil
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	killed, killErr := ctrl.Stop(ctx, cmd, verbose)
	if killErr != nil && !killed {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
		return helpers.ErrCommandFailed
	}
	if !killed {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(msgDaemonWasNotRunning))
		return nil
	}
	if killErr != nil {
		// killed=true but the exit-wait timed out: SIGTERM was delivered but we
		// couldn't confirm the old process actually exited. Bailing out here
		// would reproduce the original symptom (binary updated, no
		// daemon running, nothing attempted to fix it) — so still try Start()
		// rather than giving up outright.
		cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warning"), fmt.Sprintf("old daemon did not confirm exit (%v) — attempting to start the new daemon anyway", killErr))
	}

	if err := ctrl.Start(ctx, cmd, true, false, verbose); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
		if killErr != nil {
			cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("hint:"), "the previous daemon process may still be alive — check with 'ps' and stop it manually, then run 'eos daemon start'")
		}
		cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render(ctrl.LogsHint()) + ui.TextMuted.Render(msgCheckDaemonLogs) + "\n")
		return helpers.ErrCommandFailed
	}

	cleanupTempDir(cmd, tempDir)

	cmd.Printf("\n%s %s %s\n", ui.LabelSuccess.Render("success"), msgEosUpdatedTo, ui.TextBold.Render(latestVersion))
	if os.Getuid() == 0 {
		cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), ui.TextMuted.Render("this only restarted the invoking user's daemon — other users on this host may still be running the pre-update binary"))
		cmd.PrintErr(ui.TextMuted.Render(msgRunHint) + ui.TextCommand.Render("eos daemon info --all") + ui.TextMuted.Render(" to check every user's daemon") + "\n")
	}
	return nil
}

// validateUpdatePreconditions checks the install dir is an accessible, writable
// directory and the running version is a real release tag before updating.
func validateUpdatePreconditions(cmd *cobra.Command, installDir, version string) error {
	fileInfo, err := os.Stat(installDir)
	if err != nil || !fileInfo.IsDir() {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("directory %q is not accessible", installDir))
		return helpers.ErrCommandFailed
	}

	if err := checkWritable(cmd, installDir); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("checking destination file: %v", err))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}

	if version == "dev" {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "updating not supported for dev builds")
		cmd.PrintErr(ui.TextMuted.Render("  this binary has no version stamped in it, so eos can't tell what to update from") + "\n")
		cmd.PrintErr(ui.TextMuted.Render("  download the latest release manually: ") + ui.TextCommand.Render("https://github.com/Elysium-Labs-EU/eos/releases/latest") + "\n\n")
		return helpers.ErrCommandFailed
	}

	if !strings.HasPrefix(version, "v") {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "invalid version tag, must start with 'v'")
		return helpers.ErrCommandFailed
	}

	if !semver.IsValid(version) {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "invalid semantic version")
		return helpers.ErrCommandFailed
	}

	return nil
}

// installUpdatedBinary backs up the current binary, replaces it with the
// downloaded one, and makes it executable, cleaning up on failure.
func installUpdatedBinary(cmd *cobra.Command, binary *os.File, binaryPath, tempDir string) error {
	perm := os.FileMode(0o755)
	if info, statErr := os.Stat(binaryPath); statErr == nil {
		perm = installExecutablePerm(info.Mode())
	}

	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, time.Now().Format("20060102_150405"))
	if err := createDestinationFile(backupPath); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating destination file: %v", err))
	}

	if err := copyFile(binaryPath, backupPath); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("backing up current binary: %v", err))
		cleanupTempDir(cmd, tempDir)
		return helpers.ErrCommandFailed
	}

	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("info"), "backup created at", ui.TextMuted.Render(backupPath))

	if err := replaceBinary(binary.Name(), binaryPath); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("installing new binary: %v", err))
		cleanupTempDir(cmd, tempDir)
		return helpers.ErrCommandFailed
	}
	if err := os.Chmod(binaryPath, perm); err != nil { // #nosec G302 -- perm is capped at 0755 by installExecutablePerm, never wider than the binary's own previous mode
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("setting permissions: %v", err))
		return helpers.ErrCommandFailed
	}

	return nil
}

// cleanupTempDir removes a download temp dir, printing a manual-removal hint if
// removal fails. Used across update failure and success paths.
func cleanupTempDir(cmd *cobra.Command, tempDir string) {
	if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil { // #nosec G703 -- tempDir is an internally-created os.MkdirTemp path, not user input
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("cleanup of %s failed, manual removal advised: %v", tempDir, cleanupErr))
	}
}

// installExecutablePerm returns the permission bits to apply to the freshly
// installed binary: whatever the previously installed binary already had,
// capped at 0755 so an update can never widen access beyond owner-rwx/
// group-rx/other-rx, with owner-execute guaranteed even if the prior file
// was somehow not executable. replaceBinary's os.Rename drops the old
// inode's mode entirely, so this must be read from the file before it's
// replaced.
func installExecutablePerm(existing os.FileMode) os.FileMode {
	return (existing.Perm() & 0o755) | 0o100
}

func copyFile(src, dst string) (err error) {
	source, err := os.Open(filepath.Clean(src)) // #nosec G703 -- src is constructed internally, not from user input
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if sourceCloseErr := source.Close(); sourceCloseErr != nil && err == nil {
			err = fmt.Errorf("errored closing the source file: %w", sourceCloseErr)
		}
	}()

	destination, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_TRUNC, 0644) // #nosec G302 -- backup file should be readable by all users
	if err != nil {
		return fmt.Errorf("failed to open destination file: %w", err)
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

func createDestinationFile(dst string) error {
	destination, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}
	defer func() {
		if destinationErr := destination.Close(); destinationErr != nil && err == nil {
			err = fmt.Errorf("closing destination file: %w", destinationErr)
		}
	}()
	return nil
}

func replaceBinary(src, dst string) (err error) {
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

// resolveUpdateTarget fetches the newest release and picks the asset for this
// platform. proceed is false (with a nil error) when already on the latest
// version.
func resolveUpdateTarget(ctx context.Context, cmd *cobra.Command, fetchRelease func(context.Context, bool) (*Release, error), version, userArch, userOS string, includePre bool) (result UpdateResult, proceed bool, err error) {
	release, err := fetchRelease(ctx, includePre)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("fetching latest release: %v", err))
		return UpdateResult{}, false, helpers.ErrCommandFailed
	}

	result, err = checkForUpdates(release, version, userArch, userOS)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("no compatible asset found for %s-%s", userOS, userArch))
		cmd.PrintErrf(fmtIndentLabelMsg, ui.TextMuted.Render("supported platforms:"), strings.Join(supportedPlatforms, ", "))
		return UpdateResult{}, false, helpers.ErrCommandFailed
	}

	if result.LatestVersion == "" {
		cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), "already on the latest version", ui.TextMuted.Render(fmt.Sprintf("(%s)", version)))
		return UpdateResult{}, false, nil
	}

	return result, true, nil
}

type UpdateResult struct {
	Asset          *Asset
	ChecksumsAsset *Asset
	SignatureAsset *Asset
	LatestVersion  string
}

func checkForUpdates(release *Release, current string, arch string, os string) (result UpdateResult, err error) {
	latest := release.TagName

	if semver.Compare(current, latest) >= 0 {
		return UpdateResult{}, nil
	}

	var usableAsset *Asset
	var checksumsAsset *Asset
	var signatureAsset *Asset
	for i, asset := range release.Assets {
		if strings.Contains(asset.Name, arch) && strings.Contains(asset.Name, os) {
			usableAsset = &release.Assets[i]
		}
		if asset.Name == "sha256sums.txt" {
			checksumsAsset = &release.Assets[i]
		}
		if asset.Name == "sha256sums.txt.sig" {
			signatureAsset = &release.Assets[i]
		}
	}

	if usableAsset == nil {
		return UpdateResult{}, fmt.Errorf("no usable asset found")
	}

	return UpdateResult{Asset: usableAsset, ChecksumsAsset: checksumsAsset, SignatureAsset: signatureAsset, LatestVersion: latest}, nil
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Assets     []Asset `json:"assets"`
	Prerelease bool    `json:"prerelease"`
}

// errReleaseNotFound signals that GitHub's /releases/latest 404'd, which
// happens when every published release is a prerelease (that endpoint only
// ever returns the newest non-prerelease, non-draft release).
var errReleaseNotFound = errors.New("release not found")

// fetchLatestRelease picks the release to update to. For the plain path it
// trusts GitHub's own "latest" resolution, falling back to the full release
// list (see pickLatestRelease) only when /releases/latest 404s because every
// release is a prerelease. The --pre path always walks the full list since
// GitHub does not expose a "latest including prereleases" endpoint and the
// list is not guaranteed to be sorted.
func fetchLatestRelease(ctx context.Context, includePre bool) (*Release, error) {
	if !includePre {
		release, err := fetchLatestStableRelease(ctx)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, errReleaseNotFound) {
			return nil, err
		}
	}

	releases, err := fetchAllReleases(ctx)
	if err != nil {
		return nil, err
	}
	if includePre {
		// --pre wants the newest release regardless of stable/prerelease
		// status; do not route through pickLatestRelease, whose
		// stable-preferring logic would discard a newer prerelease
		// whenever any stable release exists.
		if best := highestByTag(releases, true); best != nil {
			return best, nil
		}
		return nil, fmt.Errorf("no releases found")
	}
	return pickLatestRelease(releases)
}

// pickLatestRelease returns the highest stable release by semver, falling
// back to the highest prerelease only when no stable release exists in the
// list.
func pickLatestRelease(releases []Release) (*Release, error) {
	if best := highestByTag(releases, false); best != nil {
		return best, nil
	}
	if best := highestByTag(releases, true); best != nil {
		return best, nil
	}
	return nil, fmt.Errorf("no releases found")
}

// highestByTag returns the release with the highest valid semver tag.
// includePrerelease also considers releases flagged as prerelease.
func highestByTag(releases []Release, includePrerelease bool) *Release {
	var best *Release
	for i := range releases {
		r := &releases[i]
		if r.Prerelease && !includePrerelease {
			continue
		}
		if !semver.IsValid(r.TagName) {
			continue
		}
		if best == nil || semver.Compare(r.TagName, best.TagName) > 0 {
			best = r
		}
	}
	return best
}

func fetchAllReleases(ctx context.Context) ([]Release, error) {
	resp, err := fetchGitHubAPI(ctx, "https://api.github.com/repos/Elysium-Labs-EU/eos/releases")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}
	return releases, nil
}

func fetchGitHubAPI(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request building failed: %w", err)
	}
	req.Header.Set(headerUserAgent, updateUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req) // #nosec G704 -- URL is constructed from hardcoded GitHub API base, not user input
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	return resp, nil
}

func fetchLatestStableRelease(ctx context.Context) (*Release, error) {
	resp, err := fetchGitHubAPI(ctx, "https://api.github.com/repos/Elysium-Labs-EU/eos/releases/latest")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errReleaseNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &release, nil
}

func handleDownloadBinary(ctx context.Context, latestAsset *Asset) (_ *os.File, tempDir string, err error) {
	resp, err := fetchAssetResponse(ctx, latestAsset)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		err = sysCombineCloseErr(resp.Body.Close(), err)
	}()

	cleanUpRequiredOnError := true
	tempDir, err = os.MkdirTemp("", "tempDownloadDir")
	if err != nil {
		return nil, "", fmt.Errorf("unable to create temporary download directory for downloading binary: %w", err)
	}
	defer func() {
		if cleanUpRequiredOnError {
			err = sysCombineCleanupErr(err, os.RemoveAll(tempDir))
		}
	}()

	file, err := writeResponseToTempFile(resp, tempDir, latestAsset.Name)
	if err != nil {
		return nil, "", err
	}

	cleanUpRequiredOnError = false
	return file, tempDir, nil
}

// fetchAssetResponse validates the asset URL (https + github.com), issues the
// GET, and returns a non-nil 200 response whose Body the caller must close. It
// closes the body itself on any non-success path.
func fetchAssetResponse(ctx context.Context, latestAsset *Asset) (*http.Response, error) {
	parsedURL, err := url.Parse(latestAsset.BrowserDownloadURL)
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Hostname(), "github.com") {
		return nil, fmt.Errorf("invalid URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAsset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("request building failed: %w", err)
	}
	req.Header.Set(headerUserAgent, updateUserAgent)

	resp, err := httpClient.Do(req) // #nosec G704 -- URL is constructed from hardcoded GitHub API base, not user input
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return resp, nil
}

// writeResponseToTempFile streams resp.Body into a new file under tempDir,
// verifies the received size against Content-Length, and rewinds the file so
// the caller can read it back.
func writeResponseToTempFile(resp *http.Response, tempDir, name string) (*os.File, error) {
	file, err := os.Create(filepath.Clean(filepath.Join(tempDir, name)))
	if err != nil {
		return nil, fmt.Errorf("errored during creating file for downloading binary: %w", err)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("errored during copying contents of fetched binary: %w", err)
	}

	if resp.ContentLength != -1 && written != resp.ContentLength {
		return nil, fmt.Errorf("received file doesn't match expected size")
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset seeker on the file: %w", err)
	}
	return file, nil
}

// sysCombineCloseErr folds a response-body close error into err, without
// clobbering an earlier, more specific failure. Pure — no I/O.
func sysCombineCloseErr(closeErr, err error) error {
	if closeErr != nil && err == nil {
		return fmt.Errorf("closing response body: %w", closeErr)
	}
	return err
}

// sysCombineCleanupErr folds a temp-dir cleanup error into err, without
// clobbering an earlier, more specific failure. Pure — no I/O.
func sysCombineCleanupErr(err, cleanUpErr error) error {
	if cleanUpErr == nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("%w; cleanup also failed: %w", err, cleanUpErr)
	}
	return fmt.Errorf("cleaning up temporary directory: %w", cleanUpErr)
}

func fetchChecksumForBinary(ctx context.Context, checksumsAsset *Asset, binaryName string) (string, error) {
	if checksumsAsset == nil {
		return "", fmt.Errorf("no sha256sums.txt asset in release")
	}

	parsedURL, err := url.Parse(checksumsAsset.BrowserDownloadURL)
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Hostname(), "github.com") {
		return "", fmt.Errorf("invalid checksums URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsAsset.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(headerUserAgent, updateUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching sha256sums.txt: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("nil response fetching sha256sums.txt")
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response, close error not actionable
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status fetching sha256sums.txt: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == binaryName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading sha256sums.txt: %w", err)
	}

	return "", fmt.Errorf("no checksum found for %q in sha256sums.txt", binaryName)
}
