package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const initSchemaHeader = "# yaml-language-server: $schema=https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/service.schema.json\n\n"

const initLogSinkHint = "\n# Optional: route logs to a sink plugin.\n# log_sinks:\n#   - type: logbench\n#     options:\n#       project_id: \"your-project-id\"\n"

// initServiceConfig mirrors types.ServiceConfig but with Runtime as a pointer
// so yaml omitempty works; an empty Runtime struct would otherwise marshal to "runtime: {}".
type initServiceConfig struct {
	Runtime       *types.Runtime `yaml:"runtime,omitempty"`
	Name          string         `yaml:"name"`
	Command       string         `yaml:"command"`
	EnvFile       string         `yaml:"env_file,omitempty"`
	Port          int            `yaml:"port,omitempty"`
	MemoryLimitMb int            `yaml:"memory_limit_mb,omitempty"`
}

type runtimeDetection struct {
	runtimeType   string
	suggestedPath string
}

// initCmdRuntimeProbe holds raw filesystem signals gathered from a project
// directory; keeping it separate from runtimeDetection lets the detection
// logic in initCmdDecideRuntime stay pure and testable without touching disk.
type initCmdRuntimeProbe struct {
	nodeVersion     string
	pythonVersion   string
	hasBun          bool
	hasDeno         bool
	hasPackageJSON  bool
	hasPythonMarker bool
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Generate a service.yaml for a project",
		Long:  `Interactively generate a service.yaml in the target directory. Detects runtime from project files to prefill defaults.`,
		Example: `  eos init              # generate service.yaml in current directory
  eos init ./myproject  # generate in a specific directory`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving path: %v", err))
				return helpers.ErrCommandFailed
			}

			if _, statErr := os.Stat(absDir); os.IsNotExist(statErr) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("directory does not exist: %s", absDir))
				return helpers.ErrCommandFailed
			}

			outputPath := filepath.Join(absDir, "service.yaml")

			// single reader for all prompts; prevents buffering skew between confirm and wizard
			reader := bufio.NewReader(cmd.InOrStdin())

			force, _ := cmd.Flags().GetBool("force")
			if !initCmdConfirmOverwrite(cmd, reader, outputPath, absDir, force) {
				cmd.Printf("  init canceled\n\n")
				return nil
			}

			detected := detectRuntime(absDir)

			cmd.Printf("\n%s service.yaml\n\n", ui.LabelStep.Render("init"))

			name, command, advanced, port := initCmdPromptBasics(cmd, reader, absDir)

			cfg := initServiceConfig{
				Name:    name,
				Command: command,
				Port:    port,
			}

			if advanced {
				initCmdApplyAdvancedAnswers(cmd, reader, detected, &cfg)
			}

			if err := initCmdWriteServiceFile(cmd, outputPath, cfg); err != nil {
				return err
			}

			initCmdPrintSuccess(cmd, outputPath)
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "overwrite existing service.yaml without prompting")

	return cmd
}

// initCmdConfirmOverwrite reports whether service.yaml creation should proceed.
// It returns false only when the file exists, force is unset, and the user declines.
func initCmdConfirmOverwrite(cmd *cobra.Command, reader *bufio.Reader, outputPath, absDir string, force bool) bool {
	if _, existErr := os.Stat(outputPath); existErr != nil {
		return true
	}

	if force {
		cmd.Printf("  %s overwriting existing service.yaml\n\n", ui.LabelWarning.Render("warning"))
		return true
	}

	cmd.Printf("  %s service.yaml already exists in %s\n\n", ui.LabelWarning.Render("warning"), absDir)
	answer := promptLine(cmd, reader, "overwrite? (y/n)", "n")
	return answer == "y" || answer == "yes"
}

func initCmdPromptBasics(cmd *cobra.Command, reader *bufio.Reader, absDir string) (name, command string, advanced bool, port int) {
	name = promptLine(cmd, reader, "service name", filepath.Base(absDir))
	command = promptLine(cmd, reader, "command (blank = skip)", "")

	mode := promptLine(cmd, reader, "mode (s=simple / a=advanced)", "s")
	advanced = strings.TrimSpace(strings.ToLower(mode)) == "a"

	portStr := promptLine(cmd, reader, "port (blank = skip)", "")
	port, _ = strconv.Atoi(strings.TrimSpace(portStr))

	return name, command, advanced, port
}

func initCmdApplyAdvancedAnswers(cmd *cobra.Command, reader *bufio.Reader, detected runtimeDetection, cfg *initServiceConfig) {
	runtimeType := promptLine(cmd, reader, "runtime type", detected.runtimeType)
	runtimePath := promptLine(cmd, reader, "runtime path", detected.suggestedPath)
	envFile := promptLine(cmd, reader, "env file (blank = skip)", "")
	memStr := promptLine(cmd, reader, "memory limit mb (blank = skip)", "")
	memLimit, _ := strconv.Atoi(strings.TrimSpace(memStr))

	if runtimeType != "" && runtimePath != "" {
		cfg.Runtime = &types.Runtime{Type: runtimeType, Path: runtimePath}
	}
	cfg.EnvFile = envFile
	cfg.MemoryLimitMb = memLimit
}

func initCmdWriteServiceFile(cmd *cobra.Command, outputPath string, cfg initServiceConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("marshaling config: %v", err))
		return helpers.ErrCommandFailed
	}

	if err := os.WriteFile(outputPath, []byte(initSchemaHeader+string(data)+initLogSinkHint), 0644); err != nil { // #nosec G306 -- service.yaml is a project config file, world-readable is intentional
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing file: %v", err))
		return helpers.ErrCommandFailed
	}

	return nil
}

func initCmdPrintSuccess(cmd *cobra.Command, outputPath string) {
	cmd.Printf("\n%s %s\n\n", ui.LabelSuccess.Render("created"), outputPath)
	cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("next:"), ui.TextCommand.Render(fmt.Sprintf("eos run -f %s", outputPath)))
}

func promptLine(cmd *cobra.Command, r *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		cmd.Printf("  %s [%s]: ", ui.TextMuted.Render(label), defaultVal)
	} else {
		cmd.Printf("  %s: ", ui.TextMuted.Render(label))
	}

	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return defaultVal
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return defaultVal
	}
	return trimmed
}

func detectRuntime(dir string) runtimeDetection {
	return initCmdDecideRuntime(initCmdProbeRuntime(dir))
}

func initCmdProbeRuntime(dir string) initCmdRuntimeProbe {
	fileExists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	readFirstLine := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- reading project version pin files in user-supplied directory
		if err != nil {
			return ""
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		return strings.TrimSpace(first)
	}

	nodeVersion := readFirstLine(".nvmrc")
	if nodeVersion == "" {
		nodeVersion = readFirstLine(".node-version")
	}

	return initCmdRuntimeProbe{
		hasBun:          fileExists("bun.lockb") || fileExists("bunfig.toml"),
		hasDeno:         fileExists("deno.json") || fileExists("deno.jsonc"),
		hasPackageJSON:  fileExists("package.json"),
		nodeVersion:     nodeVersion,
		hasPythonMarker: slices.ContainsFunc([]string{"pyproject.toml", "setup.py", "requirements.txt", "Pipfile"}, fileExists),
		pythonVersion:   readFirstLine(".python-version"),
	}
}

// initCmdDecideRuntime is pure: it only inspects probe results, no filesystem access.
func initCmdDecideRuntime(probe initCmdRuntimeProbe) runtimeDetection {
	// bun checked before node; bun projects often also have package.json
	if probe.hasBun {
		return runtimeDetection{runtimeType: "bun", suggestedPath: "~/.bun/bin"}
	}

	if probe.hasDeno {
		return runtimeDetection{runtimeType: "deno", suggestedPath: "~/.deno/bin"}
	}

	if probe.hasPackageJSON {
		return runtimeDetection{runtimeType: "node", suggestedPath: initCmdNodeRuntimePath(probe.nodeVersion)}
	}

	if probe.hasPythonMarker {
		return runtimeDetection{runtimeType: "python3", suggestedPath: initCmdPythonRuntimePath(probe.pythonVersion)}
	}

	return runtimeDetection{}
}

func initCmdNodeRuntimePath(version string) string {
	if version == "" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return fmt.Sprintf("~/.nvm/versions/node/%s/bin", version)
}

func initCmdPythonRuntimePath(version string) string {
	if version == "" {
		return ""
	}
	return fmt.Sprintf("~/.pyenv/versions/%s/bin", version)
}
