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

	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func NewServiceCatalogEntry(name string, path string, configFile string) (*types.ServiceCatalogEntry, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("received an empty name for the service")
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
	if config.Name == "" {
		return nil, fmt.Errorf("service name is required in %s", cleanedConfigFilePath)
	}
	if config.Command == "" {
		return nil, fmt.Errorf("service command is required in %s", cleanedConfigFilePath)
	}

	return &config, nil
}

func ValidateRuntimeBinary(runtime types.Runtime) error {
	if runtime.Path != "" {
		return ValidateRuntimePath(runtime)
	}
	switch runtime.Type {
	case "bun":
		if _, err := exec.LookPath("bun"); err != nil {
			return fmt.Errorf("bun not found in system PATH: %w", err)
		}
	case "deno":
		if _, err := exec.LookPath("deno"); err != nil {
			return fmt.Errorf("deno not found in system PATH: %w", err)
		}
	case "node", "nodejs":
		if _, err := exec.LookPath("node"); err != nil {
			return fmt.Errorf("node not found in system PATH: %w", err)
		}
	case "python", "python3":
		if _, err := exec.LookPath("python3"); err != nil {
			if _, err := exec.LookPath("python"); err != nil {
				return fmt.Errorf("python/python3 not found in system PATH: %w", err)
			}
		}
	}
	return nil
}

func ValidateServiceConfig(configFilePath string) (*types.ServiceConfig, []error) {
	if len(configFilePath) == 0 {
		return nil, []error{fmt.Errorf("configFilePath is empty")}
	}
	cleanedConfigFilePath := filepath.Clean(configFilePath)
	data, err := os.ReadFile(cleanedConfigFilePath)
	if err != nil {
		return nil, []error{fmt.Errorf("reading file: %w", err)}
	}
	var config types.ServiceConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, []error{fmt.Errorf("yaml parsing: %w", err)}
	}

	var errs []error
	if config.Name == "" {
		errs = append(errs, fmt.Errorf("service name is required"))
	}
	if config.Command == "" {
		errs = append(errs, fmt.Errorf("service command is required"))
	}
	if err := ValidateRuntimeBinary(config.Runtime); err != nil {
		errs = append(errs, fmt.Errorf("runtime: %w", err))
	}
	for i := range config.LogSinks {
		ref := &config.LogSinks[i]
		if ref.Inline == nil {
			// Name reference into the daemon's sink registry; the registry
			// isn't in scope during standalone service.yaml validation, so
			// resolution and validation of the referenced sink happens at
			// service start time via ResolveLogSinks.
			continue
		}
		if sinkErrs := ValidateLogSink(ref.Inline); len(sinkErrs) > 0 {
			for _, e := range sinkErrs {
				errs = append(errs, fmt.Errorf("log_sinks[%d]: %w", i, e))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return &config, nil
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
	if sink.Exec != "" {
		if _, err := exec.LookPath(sink.Exec); err != nil {
			if _, statErr := os.Stat(sink.Exec); statErr != nil {
				errs = append(errs, fmt.Errorf("exec %q not found: %w", sink.Exec, err))
			}
		}
	} else {
		binaryName := "eos-sink-" + sink.Type
		if _, err := exec.LookPath(binaryName); err != nil {
			errs = append(errs, fmt.Errorf("plugin binary %q not found on PATH (set exec: to override)", binaryName))
		}
	}
	if sink.BufferSize < 0 {
		errs = append(errs, fmt.Errorf("buffer_size must be >= 0"))
	}
	if sink.RestartDelayMs < 0 {
		errs = append(errs, fmt.Errorf("restart_delay_ms must be >= 0"))
	}
	for _, s := range sink.Streams {
		if !validStreams[s] {
			errs = append(errs, fmt.Errorf("streams: %q is invalid (must be stdout or stderr)", s))
		}
	}
	return errs
}
