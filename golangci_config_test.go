package main

import (
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGolangciModernizeEnabled guards against the modernize linter silently
// dropping out of .golangci.yml again (issue #47): gopls surfaces modernize
// suggestions live in the IDE, but only CI/pre-commit catch them if the
// linter is actually enabled here.
func TestGolangciModernizeEnabled(t *testing.T) {
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
		t.Error(".golangci.yml linters.enable must contain \"modernize\"")
	}
}
