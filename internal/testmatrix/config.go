// Package testmatrix orchestrates eos's cross-distro OrbStack e2e test matrix:
// cloning golden VMs per target, running suites against each clone in
// parallel, collecting results, and tearing clones down (or keeping them on
// failure for debugging).
package testmatrix

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// Target is one distro/init-system combination tested against its own
// long-lived golden OrbStack VM.
type Target struct {
	Name   string `yaml:"name"`
	Distro string `yaml:"distro"`
	Init   string `yaml:"init"`
	Golden string `yaml:"golden"`
}

// Suite is one test command run inside a target's clone.
type Suite struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Only    []string `yaml:"only,omitempty"`
	Nightly bool     `yaml:"nightly,omitempty"`
}

// Config is the full test matrix: which targets exist and which suites run
// against them.
type Config struct {
	Targets []Target `yaml:"targets"`
	Suites  []Suite  `yaml:"suites"`
}

// AppliesTo reports whether suite s should run against target name, honoring
// the suite's Only allowlist (empty Only means all targets).
func (s Suite) AppliesTo(name string) bool {
	if len(s.Only) == 0 {
		return true
	}
	return slices.Contains(s.Only, name)
}

// LoadConfig reads and parses a matrix config file at path.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-controlled, not user input
	if err != nil {
		return Config{}, fmt.Errorf("read matrix config %s: %w", path, err)
	}

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse matrix config %s: %w", path, err)
	}

	names, err := validateTargets(path, cfg.Targets)
	if err != nil {
		return Config{}, err
	}
	if err := validateSuites(path, cfg.Suites, names); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// validateTargets checks Targets is non-empty and each entry has a name and
// golden VM with no duplicate names, returning the name set for suite
// validation.
func validateTargets(path string, targets []Target) (map[string]bool, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("matrix config %s: no targets defined", path)
	}

	names := make(map[string]bool, len(targets))
	for _, t := range targets {
		if t.Name == "" || t.Golden == "" {
			return nil, fmt.Errorf("matrix config %s: target missing name or golden", path)
		}
		if names[t.Name] {
			return nil, fmt.Errorf("matrix config %s: duplicate target %q", path, t.Name)
		}
		names[t.Name] = true
	}
	return names, nil
}

// validateSuites checks Suites is non-empty and each entry has a name and
// command, and that any Only entries reference a known target name.
func validateSuites(path string, suites []Suite, targetNames map[string]bool) error {
	if len(suites) == 0 {
		return fmt.Errorf("matrix config %s: no suites defined", path)
	}

	for _, s := range suites {
		if s.Name == "" || s.Command == "" {
			return fmt.Errorf("matrix config %s: suite missing name or command", path)
		}
		for _, only := range s.Only {
			if !targetNames[only] {
				return fmt.Errorf("matrix config %s: suite %q references unknown target %q", path, s.Name, only)
			}
		}
	}
	return nil
}
