package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/cronutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func NewServiceCatalogEntry(name, path, configFile string) (*types.ServiceCatalogEntry, error) {
	if err := ValidateServiceName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("received an empty path for the service")
	}
	if strings.TrimSpace(configFile) == "" {
		return nil, fmt.Errorf("received an empty configFile for the service")
	}

	serviceCatalogEntry := &types.ServiceCatalogEntry{
		Name:           name,
		DirectoryPath:  path,
		ConfigFileName: configFile,
		CreatedAt:      time.Now(),
	}

	return serviceCatalogEntry, nil
}

func LoadServiceConfig(configFilePath string) (*types.ServiceConfig, error) {
	if len(configFilePath) == 0 {
		return nil, fmt.Errorf("configFilePath is empty, got %s", configFilePath)
	}
	cleanedConfigFilePath := filepath.Clean(configFilePath)
	data, err := os.ReadFile(cleanedConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading configFilePath has failed with: %w", err)
	}
	var config types.ServiceConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("yaml parsing failed with: %w", err)
	}
	if err := ValidateServiceName(config.Name); err != nil {
		return nil, fmt.Errorf("invalid service name in %s: %w", cleanedConfigFilePath, err)
	}
	if config.Command == "" {
		return nil, fmt.Errorf("service command is required in %s", cleanedConfigFilePath)
	}

	return &config, nil
}

// maxServiceNameLength keeps names well under typical filesystem filename
// limits (255 bytes) even after CreateOutputLogFilename/CreateErrorOutputLogFilename
// append their "-out.log"/"-error.log" suffixes.
const maxServiceNameLength = 128

var validServiceName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateServiceName checks that a service name is safe to use as a path
// component: it becomes part of log filenames (CreateOutputLogFilename,
// CreateErrorOutputLogFilename) and is joined onto the log directory with
// filepath.Join. Restricting to a fixed charset (no '/', no '.') makes
// directory traversal via a name like "../../pwned" impossible by
// construction, rather than trying to blocklist "..".
func ValidateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	if len(name) > maxServiceNameLength {
		return fmt.Errorf("service name %q exceeds maximum length of %d characters", name, maxServiceNameLength)
	}
	if !validServiceName.MatchString(name) {
		return fmt.Errorf("service name %q is invalid: only letters, digits, '_' and '-' are allowed", name)
	}
	return nil
}

func ValidateRuntimeBinary(runtime types.Runtime) error {
	if runtime.Path != "" {
		return ValidateRuntimePath(runtime)
	}
	switch runtime.Type {
	case "bun":
		return cfgvLookupRuntimeBinary("bun")
	case "deno":
		return cfgvLookupRuntimeBinary("deno")
	case "node", "nodejs":
		return cfgvLookupRuntimeBinary("node")
	case "python", "python3":
		return cfgvLookupPythonBinary()
	}
	return nil
}

func cfgvLookupRuntimeBinary(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found in system PATH: %w", name, err)
	}
	return nil
}

func cfgvLookupPythonBinary() error {
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			return fmt.Errorf("python/python3 not found in system PATH: %w", err)
		}
	}
	return nil
}

func ValidateServiceConfig(configFilePath string) (*types.ServiceConfig, []error) {
	if len(configFilePath) == 0 {
		return nil, []error{fmt.Errorf("configFilePath is empty")}
	}
	config, err := cfgvLoadAndParseConfig(configFilePath)
	if err != nil {
		return nil, []error{err}
	}

	var errs []error
	errs = append(errs, cfgvValidateServiceFields(config)...)
	errs = append(errs, cfgvValidateInlineLogSinks(config.LogSinks)...)
	if len(errs) > 0 {
		return nil, errs
	}
	return config, nil
}

func cfgvLoadAndParseConfig(configFilePath string) (*types.ServiceConfig, error) {
	cleanedConfigFilePath := filepath.Clean(configFilePath)
	data, err := os.ReadFile(cleanedConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	var config types.ServiceConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("yaml parsing: %w", err)
	}
	return &config, nil
}

func cfgvValidateServiceFields(config *types.ServiceConfig) []error {
	var errs []error
	if err := ValidateServiceName(config.Name); err != nil {
		errs = append(errs, err)
	}
	if config.Command == "" {
		errs = append(errs, fmt.Errorf("service command is required"))
	}
	if err := ValidateRuntimeBinary(config.Runtime); err != nil {
		errs = append(errs, fmt.Errorf("runtime: %w", err))
	}
	if err := ValidateCronRestart(config.CronRestart); err != nil {
		errs = append(errs, fmt.Errorf("cron_restart: %w", err))
	}
	if depErrs := ValidateDependencies(config.Name, config.DependsOn, config.MaxWait); len(depErrs) > 0 {
		errs = append(errs, depErrs...)
	}
	return errs
}

// cfgvValidateInlineLogSinks validates inline log_sinks entries. Name
// references into the daemon's sink registry are skipped; the registry isn't
// in scope during standalone service.yaml validation, so resolution and
// validation of a referenced sink happens at service start time via
// ResolveLogSinks.
func cfgvValidateInlineLogSinks(refs []types.LogSinkRef) []error {
	var errs []error
	for i := range refs {
		ref := &refs[i]
		if ref.Inline == nil {
			continue
		}
		if sinkErrs := ValidateLogSink(ref.Inline); len(sinkErrs) > 0 {
			for _, e := range sinkErrs {
				errs = append(errs, fmt.Errorf("log_sinks[%d]: %w", i, e))
			}
		}
	}
	return errs
}

// ValidateCronRestart validates the optional cron_restart field. An empty
// expression is valid (cron restart disabled for that service).
func ValidateCronRestart(cronExpr string) error {
	if cronExpr == "" {
		return nil
	}
	if _, err := cronutil.ParseSchedule(cronExpr); err != nil {
		return err
	}
	return nil
}

// ValidateDependencies checks a service's depends_on / max_wait pair. A service
// naming itself, or the same dependency twice, is a config mistake rather than a
// runtime condition, so it fails at validation instead of hanging until max_wait.
func ValidateDependencies(serviceName string, dependsOn []string, maxWait string) []error {
	var errs []error
	if _, err := ParseMaxWait(maxWait); err != nil {
		errs = append(errs, fmt.Errorf("max_wait: %w", err))
	}
	seen := make(map[string]bool, len(dependsOn))
	for _, dep := range dependsOn {
		if strings.TrimSpace(dep) == "" {
			errs = append(errs, fmt.Errorf("depends_on: empty dependency name"))
			continue
		}
		if dep == serviceName {
			errs = append(errs, fmt.Errorf("depends_on: service %q cannot depend on itself", serviceName))
			continue
		}
		if seen[dep] {
			errs = append(errs, fmt.Errorf("depends_on: duplicate dependency %q", dep))
			continue
		}
		seen[dep] = true
	}
	return errs
}

var selfDetachCommands = map[string]bool{"setsid": true, "nohup": true, "disown": true}

var commandSeparators = regexp.MustCompile(`&&|\|\||[;|]`)

// DetectSelfDetachRisk flags command segments that start with a self-detaching
// command (setsid, nohup, disown). eos tracks the process it spawns via a
// single process group (Setpgid: true) and kills that group on stop; a
// segment that detaches escapes the group and eos loses the ability to
// stop/kill it. This is a string heuristic on the configured command, not a
// runtime check — it won't catch a program that daemonizes internally.
func DetectSelfDetachRisk(command string) []string {
	var warnings []string
	for _, segment := range commandSeparators.Split(command, -1) {
		fields := strings.Fields(segment)
		if len(fields) == 0 {
			continue
		}
		if selfDetachCommands[fields[0]] {
			warnings = append(warnings, fmt.Sprintf(
				"command segment %q starts with %q, which detaches from eos's process group; eos will not be able to stop or kill it via the normal service commands",
				strings.TrimSpace(segment), fields[0],
			))
		}
	}
	return warnings
}

// ResolveLogSinks resolves a service's log_sinks entries against the
// daemon's named sink registry (~/.eos/config.yaml sinks:). Inline sink
// configs pass through unchanged; name references are looked up in
// registry. An unknown name is a hard error; sinks are how logs leave the
// system, so a typo should fail loudly at start time rather than silently
// drop a sink.
func ResolveLogSinks(serviceName string, refs []types.LogSinkRef, registry map[string]types.LogSink) ([]types.LogSink, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	resolved := make([]types.LogSink, 0, len(refs))
	for i, ref := range refs {
		if ref.Inline != nil {
			resolved = append(resolved, *ref.Inline)
			continue
		}
		sink, ok := registry[ref.Name]
		if !ok {
			return nil, fmt.Errorf("service '%s': log_sinks[%d]: unknown sink %q — registered: %s", serviceName, i, ref.Name, formatRegisteredSinkNames(registry))
		}
		resolved = append(resolved, sink)
	}
	return resolved, nil
}

func formatRegisteredSinkNames(registry map[string]types.LogSink) string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return "[" + strings.Join(names, ", ") + "]"
}

var validStreams = map[string]bool{"stdout": true, "stderr": true}

func ValidateLogSink(sink *types.LogSink) []error {
	var errs []error
	if sink.Type == "" {
		errs = append(errs, fmt.Errorf("type is required"))
		return errs
	}
	if err := cfgvValidateSinkBinary(sink); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, cfgvValidateSinkNumericFields(sink)...)
	errs = append(errs, cfgvValidateSinkStreams(sink.Streams)...)
	return errs
}

func cfgvValidateSinkBinary(sink *types.LogSink) error {
	if sink.Exec != "" {
		if _, err := exec.LookPath(sink.Exec); err != nil {
			if _, statErr := os.Stat(sink.Exec); statErr != nil {
				return fmt.Errorf("exec %q not found: %w", sink.Exec, err)
			}
		}
		return nil
	}
	binaryName := "eos-sink-" + sink.Type
	if _, err := exec.LookPath(binaryName); err != nil {
		return fmt.Errorf("plugin binary %q not found on PATH (set exec: to override)", binaryName)
	}
	return nil
}

func cfgvValidateSinkNumericFields(sink *types.LogSink) []error {
	var errs []error
	if sink.BufferSize < 0 {
		errs = append(errs, fmt.Errorf("buffer_size must be >= 0"))
	}
	if sink.RestartDelayMs < 0 {
		errs = append(errs, fmt.Errorf("restart_delay_ms must be >= 0"))
	}
	return errs
}

func cfgvValidateSinkStreams(streams []string) []error {
	var errs []error
	for _, s := range streams {
		if !validStreams[s] {
			errs = append(errs, fmt.Errorf("streams: %q is invalid (must be stdout or stderr)", s))
		}
	}
	return errs
}
