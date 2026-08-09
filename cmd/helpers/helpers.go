// Package helpers provides CLI utility functions for output formatting, JSON rendering, and shell completions.
package helpers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

// dependencyWaitStatusReader is satisfied by any manager.ServiceManager whose
// concrete type also tracks an in-flight depends_on wait (LocalManager,
// DaemonManager, via manager.RecordDependencyWait). A manager that doesn't
// (e.g. a test fake) is simply treated as "not waiting" — this is
// status-display observability, not part of the dependency gate itself.
type dependencyWaitStatusReader interface {
	GetDependencyWaitStatus(ctx context.Context, name string) (status types.DependencyWaitStatus, waiting bool, err error)
}

// ResolveDependencyWaitStatus reports the dependency names name is currently
// gated on, or nil if it isn't waiting on any right now. Status output uses
// this to render a distinct "waiting" state instead of a service blocked on
// depends_on looking identical to one that was simply never started.
func ResolveDependencyWaitStatus(ctx context.Context, mgr manager.ServiceManager, name string) []string {
	reader, ok := mgr.(dependencyWaitStatusReader)
	if !ok {
		return nil
	}
	wait, waiting, err := reader.GetDependencyWaitStatus(ctx, name)
	if err != nil || !waiting {
		return nil
	}
	return wait.Pending
}

// DetermineServiceStatus resolves the display status for a service's most
// recent process_history row. hasLiveOrphan overrides an otherwise
// inactive-looking status (stopped/failed/unknown) to "orphaned" when a
// process group from an EARLIER row is still alive — callers get this from
// manager.ServiceManager.GetLiveOrphanProcessGroups, since a status derived
// from mostRecentProcess alone would otherwise report a live process as
// stopped just because it isn't the newest row.
func DetermineServiceStatus(mostRecentProcess *types.ProcessHistory, hasLiveOrphan bool) types.ServiceStatus {
	status := determineStatusFromProcessState(mostRecentProcess)
	if !hasLiveOrphan {
		return status
	}
	switch status {
	case types.ServiceStatusStopped, types.ServiceStatusFailed, types.ServiceStatusUnknown:
		return types.ServiceStatusOrphaned
	case types.ServiceStatusRunning, types.ServiceStatusStarting, types.ServiceStatusWaitingForDeps, types.ServiceStatusOrphaned:
		return status
	default:
		return status
	}
}

func determineStatusFromProcessState(mostRecentProcess *types.ProcessHistory) types.ServiceStatus {
	if mostRecentProcess == nil {
		return types.ServiceStatusStopped
	}
	switch mostRecentProcess.State {
	case types.ProcessStateStopped:
		return types.ServiceStatusStopped
	case types.ProcessStateFailed:
		return types.ServiceStatusFailed
	case types.ProcessStateRunning:
		return types.ServiceStatusRunning
	case types.ProcessStateStarting:
		return types.ServiceStatusStarting
	case types.ProcessStateUnknown:
		return types.ServiceStatusUnknown
	default:
		return types.ServiceStatusUnknown
	}
}

func DetermineUptimeHuman(mostRecentProcess *types.ProcessHistory) string {
	if mostRecentProcess == nil {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateStopped {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateFailed {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateUnknown {
		return "-"
	}
	if mostRecentProcess.StartedAt == nil {
		return "-"
	}
	return humanize.Time(*mostRecentProcess.StartedAt)
}

func DetermineUptimeAPI(mostRecentProcess *types.ProcessHistory) *string {
	if mostRecentProcess == nil {
		return nil
	}
	if mostRecentProcess.State == types.ProcessStateStopped {
		return nil
	}
	if mostRecentProcess.State == types.ProcessStateFailed {
		return nil
	}
	if mostRecentProcess.State == types.ProcessStateUnknown {
		return nil
	}
	if mostRecentProcess.StartedAt == nil {
		return nil
	}
	return new(humanize.Time(*mostRecentProcess.StartedAt))
}

func DetermineProcessMemoryInMbHuman(rssMemoryKb int64, status types.ServiceStatus) string {
	if status == types.ServiceStatusFailed || status == types.ServiceStatusStopped || status == types.ServiceStatusOrphaned {
		return "-"
	}
	if rssMemoryKb <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MB", float64(rssMemoryKb)/1024)
}

func DetermineProcessMemoryInMbAPI(rssMemoryKb int64, status types.ServiceStatus) *string {
	if status == types.ServiceStatusFailed || status == types.ServiceStatusStopped || status == types.ServiceStatusOrphaned {
		return nil
	}
	if rssMemoryKb <= 0 {
		return nil
	}
	return new(fmt.Sprintf("%.1f MB", float64(rssMemoryKb)/1024))
}

// DetermineProcessPeakMemoryInMbHuman formats the highest RSS a service's
// current process history entry has reached. Unlike current memory, it isn't
// blanked out on stop/failure — that's the point of tracking a peak, seeing
// how high memory got even after the process is gone.
func DetermineProcessPeakMemoryInMbHuman(peakRssMemoryKb int64) string {
	if peakRssMemoryKb <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MB", float64(peakRssMemoryKb)/1024)
}

// DetermineProcessCPUHuman formats a per-service CPU percentage for status
// output. Unlike memory, 0% is a meaningful reading (an idle-but-running
// service), so a running service always shows a number; only stopped/failed
// services collapse to "-".
func DetermineProcessCPUHuman(cpuPercent float64, status types.ServiceStatus) string {
	if status == types.ServiceStatusFailed || status == types.ServiceStatusStopped || status == types.ServiceStatusOrphaned {
		return "-"
	}
	if cpuPercent < 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", cpuPercent)
}

// StaleThresholdMultiplier scales the configured health-check interval into the
// age past which a process_history row is considered stale. A healthy monitor
// rewrites updated_at every CheckInterval; allowing 3 missed ticks before
// flagging tolerates one-off scheduling jitter without hiding a frozen or
// dead-monitor row.
const StaleThresholdMultiplier = 3

// IsProcessHistoryStale reports whether the most recent process_history row has
// gone stale: its updated_at is older than StaleThresholdMultiplier times the
// configured health-check interval. This is independent of daemon-liveness
// detection; it only judges whether the monitor is still refreshing this row.
//
// Pure: caller passes the resolved interval and the reference time. Returns
// false when there is nothing to judge (nil row, nil updated_at) or when the
// interval is non-positive, so a missing threshold never marks every row stale.
func IsProcessHistoryStale(mostRecentProcess *types.ProcessHistory, checkInterval time.Duration, now time.Time) bool {
	if mostRecentProcess == nil || mostRecentProcess.UpdatedAt == nil {
		return false
	}
	if checkInterval <= 0 {
		return false
	}
	threshold := time.Duration(StaleThresholdMultiplier) * checkInterval
	return now.Sub(*mostRecentProcess.UpdatedAt) > threshold
}

// ExtractPGIDs returns the PGID of each row in history, in order. Used to
// render live-orphan process groups as a flat PGID list independent of the
// caller's own display format (table cell, JSON array, key/value line).
func ExtractPGIDs(history []types.ProcessHistory) []int {
	pgids := make([]int, len(history))
	for i := range history {
		pgids[i] = history[i].PGID
	}
	return pgids
}

// DetermineOrphanedPGIDsHuman formats the PGIDs of live process groups left
// behind by earlier process_history rows for human-readable output (table
// cell, info key/value line).
func DetermineOrphanedPGIDsHuman(pgids []int) string {
	if len(pgids) == 0 {
		return "-"
	}
	parts := make([]string, len(pgids))
	for i, pgid := range pgids {
		parts[i] = strconv.Itoa(pgid)
	}
	return strings.Join(parts, ", ")
}

func DetermineError(errorStringPtr *string) string {
	if errorStringPtr == nil {
		return "-"
	}
	if *errorStringPtr == "" {
		return "-"
	}
	return *errorStringPtr
}

func findServiceFileInDirectory(dir string) string {
	candidates := []string{
		"service.yaml",
		"service.yml",
	}

	for _, candidate := range candidates {
		fullPath := filepath.Join(dir, candidate)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}

	return ""
}

func DetermineYamlFile(projectPath string) (string, error) {
	fileInfo, err := os.Stat(projectPath)

	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("directory or file on path %s does not exist", projectPath)
		}
		return "", fmt.Errorf("unable to stat path %s: %w", projectPath, err)
	}

	if fileInfo.IsDir() {
		yamlFile := findServiceFileInDirectory(projectPath)
		if yamlFile == "" {
			return "", fmt.Errorf("no service.yaml or service.yml found in %s", projectPath)
		} else {
			return yamlFile, nil
		}
	}
	if strings.HasSuffix(projectPath, ".yaml") || strings.HasSuffix(projectPath, ".yml") {
		return projectPath, nil
	}
	return "", fmt.Errorf("provided path is not a directory nor a yaml file")
}

func PromptConfirm(cmd *cobra.Command, prompt string) (confirmed bool) {
	cmd.Printf("  %s ", ui.TextMuted.Render(prompt))

	// Read one byte at a time rather than through a bufio.Reader: a bufio.Reader
	// created fresh on every call can read ahead past the newline and buffer
	// bytes belonging to a *later* prompt's answer, which are then discarded
	// when this function returns. Over a piped (non-tty) stdin that silently
	// drops the next prompt's answer; a manual byte read never reads past '\n'.
	response, err := readLine(cmd.InOrStdin())

	if err != nil {
		// If we got io.EOF but have a response, process it anyway
		if errors.Is(err, io.EOF) && len(strings.TrimSpace(response)) > 0 {
			response = strings.TrimSpace(strings.ToLower(response))
			return response == "y" || response == "yes"
		}
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading input: %v", err))
		return
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

func readLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.WriteByte(buf[0])
			if buf[0] == '\n' {
				return sb.String(), nil
			}
		}
		if err != nil {
			return sb.String(), err
		}
	}
}

// Debugf prints a debug-labeled diagnostic line to stderr when verbose is true, no-op otherwise.
func Debugf(cmd *cobra.Command, verbose bool, format string, args ...any) {
	if !verbose {
		return
	}
	cmd.PrintErrf("%s %s\n", ui.LabelDebug.Render("debug"), fmt.Sprintf(format, args...))
}

func PrintSudoHint(cmd *cobra.Command) {
	cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run with:"), ui.TextCommand.Render("sudo"), ui.TextMuted.Render("to try again with administrative permissions"))
}

func PrintRequiresSudo(cmd *cobra.Command, action string) {
	cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), action+" requires root")
	PrintSudoHint(cmd)
}
