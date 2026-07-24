package main

import (
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGolangciConfigEnablesModernize guards against issue #47 regressing: gopls
// surfaces `modernize` suggestions live in the IDE, but unless .golangci.yml
// enables the linter, neither CI nor the pre-commit hook catch what the IDE
// flags.
func TestGolangciConfigEnablesModernize(t *testing.T) {
	raw, err := os.ReadFile(".golangci.yml")
	if err != nil {
		t.Fatalf("reading .golangci.yml: %v", err)
	}

	var cfg struct {
		Linters struct {
			Enable []string `yaml:"enable"`
		} `yaml:"linters"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing .golangci.yml: %v", err)
	}

	if !slices.Contains(cfg.Linters.Enable, "modernize") {
		t.Errorf("linters.enable = %v, want it to contain %q", cfg.Linters.Enable, "modernize")
	}
}
