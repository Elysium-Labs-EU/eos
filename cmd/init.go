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

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const initSchemaHeader = "# yaml-language-server: $schema=https://codeberg.org/Elysium_Labs/eos/raw/branch/main/schemas/service.schema.json\n\n"

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
			if _, existErr := os.Stat(outputPath); existErr == nil {
				if force {
					cmd.Printf("  %s overwriting existing service.yaml\n\n", ui.LabelWarning.Render("warning"))
				} else {
					cmd.Printf("  %s service.yaml already exists in %s\n\n", ui.LabelWarning.Render("warning"), absDir)
					answer := promptLine(cmd, reader, "overwrite? (y/n)", "n")
					if answer != "y" && answer != "yes" {
						cmd.Printf("  init canceled\n\n")
						return nil
					}
				}
			}

			detected := detectRuntime(absDir)

			cmd.Printf("\n%s service.yaml\n\n", ui.LabelStep.Render("init"))

			name := promptLine(cmd, reader, "service name", filepath.Base(absDir))

			command := promptLine(cmd, reader, "command (blank = skip)", "")

			mode := promptLine(cmd, reader, "mode (s=simple / a=advanced)", "s")
			advanced := strings.TrimSpace(strings.ToLower(mode)) == "a"

			portStr := promptLine(cmd, reader, "port (blank = skip)", "")
			port, _ := strconv.Atoi(strings.TrimSpace(portStr))

			cfg := initServiceConfig{
				Name:    name,
				Command: command,
				Port:    port,
			}

			if advanced {
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

			data, err := yaml.Marshal(cfg)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("marshaling config: %v", err))
				return helpers.ErrCommandFailed
			}

			if err := os.WriteFile(outputPath, []byte(initSchemaHeader+string(data)+initLogSinkHint), 0644); err != nil { // #nosec G306 -- service.yaml is a project config file, world-readable is intentional
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("writing file: %v", err))
				return helpers.ErrCommandFailed
			}

			cmd.Printf("\n%s %s\n\n", ui.LabelSuccess.Render("created"), outputPath)
			cmd.Printf("  %s %s\n\n", ui.TextMuted.Render("next:"), ui.TextCommand.Render(fmt.Sprintf("eos run -f %s", outputPath)))
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "overwrite existing service.yaml without prompting")

	return cmd
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

	// bun checked before node; bun projects often also have package.json
	if fileExists("bun.lockb") || fileExists("bunfig.toml") {
		return runtimeDetection{runtimeType: "bun", suggestedPath: "~/.bun/bin"}
	}

	if fileExists("deno.json") || fileExists("deno.jsonc") {
		return runtimeDetection{runtimeType: "deno", suggestedPath: "~/.deno/bin"}
	}

	if fileExists("package.json") {
		version := readFirstLine(".nvmrc")
		if version == "" {
			version = readFirstLine(".node-version")
		}
		path := ""
		if version != "" {
			if !strings.HasPrefix(version, "v") {
				version = "v" + version
			}
			path = fmt.Sprintf("~/.nvm/versions/node/%s/bin", version)
		}
		return runtimeDetection{runtimeType: "node", suggestedPath: path}
	}

	if slices.ContainsFunc([]string{"pyproject.toml", "setup.py", "requirements.txt", "Pipfile"}, fileExists) {
		version := readFirstLine(".python-version")
		path := ""
		if version != "" {
			path = fmt.Sprintf("~/.pyenv/versions/%s/bin", version)
		}
		return runtimeDetection{runtimeType: "python3", suggestedPath: path}
	}

	return runtimeDetection{}
}
