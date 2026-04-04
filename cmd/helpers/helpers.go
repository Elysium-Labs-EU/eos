package helpers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func DetermineServiceStatus(mostRecentProcess *types.ProcessHistory) types.ServiceStatus {
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

func DetermineUptime(mostRecentProcess *types.ProcessHistory) string {
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

	return humanize.Time(*mostRecentProcess.StartedAt)
}

func DetermineProcessMemoryInMb(rssMemoryKb int64) string {
	if rssMemoryKb <= 0 {
		return "-"
	}

	return fmt.Sprintf("%.1f MB", float64(rssMemoryKb)/1024)
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

	reader := bufio.NewReader(cmd.InOrStdin())
	response, err := reader.ReadString('\n')

	if err != nil {
		// If we got io.EOF but have a response, process it anyway
		if err == io.EOF && len(strings.TrimSpace(response)) > 0 {
			response = strings.TrimSpace(strings.ToLower(response))
			return response == "y" || response == "yes"
		}
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading input: %v", err))
		return
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
