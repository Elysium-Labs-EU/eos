package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// configInitTemplate scaffolds ~/.eos/config.yaml fully commented out, so the
// file documents every available field at eos's own built-in default without
// changing behavior until a line is uncommented.
const configInitTemplate = `# eos daemon configuration (~/.eos/config.yaml)
# All fields are optional. Every value below is eos's built-in default,
# commented out — uncomment and edit only the settings you want to change.
# See: eos config show (effective config), eos config validate (check this file)

# sinks:
#   prod-loki:
#     type: loki
#     mode: push
#     address: "http://loki:3100"

# telemetry:
#   enable: false
#   endpoint: ""
#   insecure: false

# health:
#   checkIntervalMs: {{.CheckIntervalMs}}
#   memSampleIntervalMs: {{.MemSampleIntervalMs}}
#   backoff:
#     baseMs: {{.BackoffBaseMs}}
#     maxMs: {{.BackoffMaxMs}}
#   memory:
#     warningThreshold: {{.WarningThreshold}}
#     softRestartThreshold: {{.SoftRestartThreshold}}
#     forceRestartThreshold: {{.ForceRestartThreshold}}

# log:
#   maxFiles: {{.LogMaxFiles}}
#   fileSizeLimitBytes: {{.LogFileSizeLimitBytes}}
`

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   cmdnames.Config,
		Short: "Inspect and scaffold the eos daemon configuration",
		Long: `View, scaffold, and validate ~/.eos/config.yaml — the daemon-wide settings for
the log sink registry, telemetry export, health thresholds, and log rotation.

This is distinct from service.yaml, which configures one registered service (see "eos init").`,
	}

	showCmd := &cobra.Command{
		Use:           cmdnames.ConfigShow,
		Short:         "Print the effective config.yaml",
		Long:          `Print the effective daemon configuration: ~/.eos/config.yaml values merged over eos's built-in defaults.`,
		Example:       `  eos config show`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd)
		},
	}

	initCmd := &cobra.Command{
		Use:           cmdnames.ConfigInit,
		Short:         "Scaffold ~/.eos/config.yaml with default values",
		Long:          `Write a fully commented ~/.eos/config.yaml showing every available field at its default value. Non-interactive; edit the file afterward to change settings.`,
		Example:       "  eos config init\n  eos config init --force  # overwrite an existing file",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			return runConfigInit(cmd, force)
		},
	}
	initCmd.Flags().Bool("force", false, "overwrite an existing config.yaml without prompting")

	validateCmd := &cobra.Command{
		Use:           cmdnames.ConfigValidate,
		Short:         "Validate ~/.eos/config.yaml",
		Long:          `Validate ~/.eos/config.yaml without starting the daemon, reporting the same error the daemon would refuse to start on.`,
		Example:       `  eos config validate`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigValidate(cmd)
		},
	}

	configCmd.AddCommand(showCmd)
	configCmd.AddCommand(initCmd)
	configCmd.AddCommand(validateCmd)

	return configCmd
}

// resolveConfigBaseDir resolves the base dir every read-only config
// subcommand needs (show, validate), printing and wrapping errors so callers
// only need to check err != nil. It deliberately does not create the
// directory — config.GetBaseDir, not config.CreateBaseDir — since a read
// should never have the side effect of creating ~/.eos.
func resolveConfigBaseDir(cmd *cobra.Command) (string, error) {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving identity: %v", err))
		return "", helpers.ErrCommandFailed
	}
	baseDir, err := config.GetBaseDir(identity)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving base dir: %v", err))
		return "", helpers.ErrCommandFailed
	}
	return baseDir, nil
}

func runConfigShow(cmd *cobra.Command) error {
	baseDir, err := resolveConfigBaseDir(cmd)
	if err != nil {
		return err
	}
	path := filepath.Join(baseDir, config.EosConfigFileName)
	_, statErr := os.Stat(path)
	fileExists := statErr == nil

	eosCfg, err := config.LoadEosConfig(baseDir)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("loading config: %v", err))
		return helpers.ErrCommandFailed
	}

	printConfigShow(cmd, path, fileExists, &eosCfg)
	return nil
}

// printConfigShow renders the effective config to cmd. Pure with respect to
// the filesystem — every value it prints was already resolved by the caller.
func printConfigShow(cmd *cobra.Command, path string, fileExists bool, cfg *config.EosConfig) {
	cmd.Println()
	cmd.Printf(fmtHeading, ui.TextBold.Render("Config"))
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("file:"), path)
	if fileExists {
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("status:"), "loaded")
	} else {
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("status:"), "not found — showing built-in defaults")
	}

	cmd.Printf(fmtHeading, ui.TextBold.Render("Sinks"))
	if len(cfg.Sinks) == 0 {
		cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("registered:"), "(none)")
	} else {
		for _, name := range sortedSinkNames(cfg.Sinks) {
			sink := cfg.Sinks[name]
			cmd.Printf("  %s %s (%s %s)\n", ui.TextMuted.Render(name+":"), sink.Type, sink.Mode, sink.Address)
		}
		cmd.Println()
	}

	cmd.Printf(fmtHeading, ui.TextBold.Render("Telemetry"))
	cmd.Printf(fmtIndentLabelAnyLn, ui.TextMuted.Render("enabled:"), cfg.Telemetry.Enable)
	if cfg.Telemetry.Enable {
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("endpoint:"), cfg.Telemetry.Endpoint)
		cmd.Printf(fmtIndentLabelAnyLn, ui.TextMuted.Render("insecure:"), cfg.Telemetry.Insecure)
	}
	cmd.Println()

	cmd.Printf(fmtHeading, ui.TextBold.Render("Health"))
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("check interval ms:"), cfg.Health.CheckIntervalMs)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("mem sample interval ms:"), cfg.Health.MemSampleIntervalMs)
	cmd.Printf("  %s %d / %d\n", ui.TextMuted.Render("backoff base/max ms:"), cfg.Health.Backoff.BaseMs, cfg.Health.Backoff.MaxMs)
	cmd.Printf("  %s %.2f / %.2f / %.2f\n\n", ui.TextMuted.Render("memory warning/soft/force:"), cfg.Health.Memory.WarningThreshold, cfg.Health.Memory.SoftRestartThreshold, cfg.Health.Memory.ForceRestartThreshold)

	cmd.Printf(fmtHeading, ui.TextBold.Render("Log"))
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("max files:"), cfg.Log.MaxFiles)
	cmd.Printf("  %s %d\n\n", ui.TextMuted.Render("file size limit bytes:"), cfg.Log.FileSizeLimitBytes)
}

func sortedSinkNames(sinks map[string]types.LogSink) []string {
	names := make([]string, 0, len(sinks))
	for name := range sinks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func runConfigInit(cmd *cobra.Command, force bool) error {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving identity: %v", err))
		return helpers.ErrCommandFailed
	}
	baseDir, err := config.CreateBaseDir(identity)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("creating eos directory: %v", err))
		return helpers.ErrCommandFailed
	}
	path := filepath.Join(baseDir, config.EosConfigFileName)

	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("%s already exists (use --force to overwrite)", path))
			return helpers.ErrCommandFailed
		}
	}

	content, err := renderConfigInitFile()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("rendering config: %v", err))
		return helpers.ErrCommandFailed
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil { // #nosec G306 -- config.yaml holds no secrets, world-readable is intentional
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("writing file: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelSuccess.Render("created"), path)
	cmd.Printf(fmtIndentLabelMsg, ui.TextMuted.Render("next:"), ui.TextCommand.Render(cmdnames.HintConfigShow))
	return nil
}

// configInitTemplateData is the pure rendering input for configInitTemplate,
// derived from config.DefaultEosConfig() so the scaffolded comments never
// drift from the defaults eos actually applies.
type configInitTemplateData struct {
	CheckIntervalMs       int
	MemSampleIntervalMs   int
	BackoffBaseMs         int
	BackoffMaxMs          int
	WarningThreshold      float64
	SoftRestartThreshold  float64
	ForceRestartThreshold float64
	LogMaxFiles           int
	LogFileSizeLimitBytes int64
}

// renderConfigInitFile renders the scaffolded config.yaml content. Pure — no I/O.
func renderConfigInitFile() (string, error) {
	def := config.DefaultEosConfig()
	data := configInitTemplateData{
		CheckIntervalMs:       def.Health.CheckIntervalMs,
		MemSampleIntervalMs:   def.Health.MemSampleIntervalMs,
		BackoffBaseMs:         def.Health.Backoff.BaseMs,
		BackoffMaxMs:          def.Health.Backoff.MaxMs,
		WarningThreshold:      def.Health.Memory.WarningThreshold,
		SoftRestartThreshold:  def.Health.Memory.SoftRestartThreshold,
		ForceRestartThreshold: def.Health.Memory.ForceRestartThreshold,
		LogMaxFiles:           def.Log.MaxFiles,
		LogFileSizeLimitBytes: def.Log.FileSizeLimitBytes,
	}

	tmpl, err := template.New("configInit").Parse(configInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}
	return buf.String(), nil
}

func runConfigValidate(cmd *cobra.Command) error {
	baseDir, err := resolveConfigBaseDir(cmd)
	if err != nil {
		return err
	}
	path := filepath.Join(baseDir, config.EosConfigFileName)

	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("%s does not exist", path))
		return helpers.ErrCommandFailed
	}

	if _, err := config.LoadEosConfig(baseDir); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("invalid"), path)
		cmd.PrintErrf("  %s %v\n\n", ui.TextMuted.Render("·"), err)
		return helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelMsg, ui.LabelSuccess.Render("valid"), path)
	return nil
}
