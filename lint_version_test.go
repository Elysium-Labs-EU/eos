package main

import (
	"os"
	"regexp"
	"testing"
)

// TestGolangciLintVersionPinsMatch guards against issue #55: Makefile's
// `setup` target and .github/workflows/golangci-lint.yml pinned different
// golangci-lint versions (v2.11.0 vs a floating v2.11 that resolved to
// v2.11.4), so the CI workflow could see staticcheck findings that `make
// lint`/`make ci` never surfaced locally.
func TestGolangciLintVersionPinsMatch(t *testing.T) {
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	workflow, err := os.ReadFile(".github/workflows/golangci-lint.yml")
	if err != nil {
		t.Fatalf("reading golangci-lint.yml: %v", err)
	}

	makefileVersionRe := regexp.MustCompile(`(?m)^\s*curl .*golangci-lint/master/install\.sh.*\s(v\d+\.\d+\.\d+)\s*$`)
	workflowVersionRe := regexp.MustCompile(`(?m)^\s*version:\s*(v\d+\.\d+\.\d+)\s*$`)

	makefileMatch := makefileVersionRe.FindSubmatch(makefile)
	if makefileMatch == nil {
		t.Fatal("could not find golangci-lint version pin in Makefile setup target")
	}
	workflowMatch := workflowVersionRe.FindSubmatch(workflow)
	if workflowMatch == nil {
		t.Fatal("could not find an exact (non-floating) golangci-lint version pin in golangci-lint.yml")
	}

	makefileVersion := string(makefileMatch[1])
	workflowVersion := string(workflowMatch[1])
	if makefileVersion != workflowVersion {
		t.Errorf("golangci-lint version pin drift: Makefile pins %s, golangci-lint.yml pins %s; keep both exact and identical", makefileVersion, workflowVersion)
	}
}
