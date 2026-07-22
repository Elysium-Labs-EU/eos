// Package cicheck guards CI workflow files against regressions that are
// hard to notice from the Go source tree alone.
package cicheck

import (
	"os"
	"strings"
	"testing"
)

// The osv-scan.yml action google/osv-scanner-action@v2.3.8 runs govulncheck
// inside a Docker image with a stale, pinned (GOTOOLCHAIN=local) Go
// toolchain: once go.mod requires a newer Go than the image ships, the scan
// fails outright regardless of the repo's actual vulnerability status. Guard
// against reintroducing that action; the pinned-binary approach below runs
// on the host runner's own Go toolchain instead.
func TestOSVScanWorkflowUsesPinnedBinaryNotDockerAction(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/osv-scan.yml")
	if err != nil {
		t.Fatalf("reading osv-scan.yml: %v", err)
	}
	workflow := string(content)

	if strings.Contains(workflow, "osv-scanner-action") {
		t.Error("osv-scan.yml must not use the google/osv-scanner-action Docker action; it ships a stale, pinned Go toolchain that breaks once go.mod requires a newer version")
	}

	if !strings.Contains(workflow, "github.com/google/osv-scanner/releases/download/") {
		t.Error("osv-scan.yml must download the pinned osv-scanner binary release directly")
	}

	if !strings.Contains(workflow, "./osv-scanner scan --lockfile=go.mod --recursive .") {
		t.Error("osv-scan.yml must run the downloaded osv-scanner binary directly, not a wrapping action")
	}
}
