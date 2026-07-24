// Package workflowcheck guards CI workflow YAML against regressions that
// only surface once GitHub/Forgejo actually run the workflow (e.g. an
// action pinned to a tag that doesn't exist).
package workflowcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type step struct {
	Uses string `yaml:"uses"`
}

type job struct {
	Steps []step `yaml:"steps"`
}

type workflow struct {
	Jobs map[string]job `yaml:"jobs"`
}

// repoRoot resolves the module root from this file's location so the test
// works regardless of the caller's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine caller for repo root resolution")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func loadWorkflow(t *testing.T, relPath string) workflow {
	t.Helper()
	path := filepath.Join(repoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return wf
}

// osv-scanner-action does not publish a floating major-version tag (e.g.
// "v2"); only fully-qualified "vX.Y.Z" release tags exist. Pinning to a
// bare major tag fails action resolution before the scan ever runs
// (github.com/Elysium-Labs-EU/eos#54).
var pinnedVersionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func TestOSVScannerActionPinnedToConcreteVersion(t *testing.T) {
	for _, wfPath := range []string{
		filepath.Join(".github", "workflows", "osv-scan.yml"),
	} {
		wf := loadWorkflow(t, wfPath)

		var found bool
		for _, j := range wf.Jobs {
			for _, s := range j.Steps {
				const prefix = "google/osv-scanner-action/osv-scanner-action@"
				if !strings.HasPrefix(s.Uses, prefix) {
					continue
				}
				found = true
				ref := s.Uses[len(prefix):]
				if !pinnedVersionRe.MatchString(ref) {
					t.Errorf("%s: osv-scanner-action pinned to %q, want a concrete vX.Y.Z tag", wfPath, ref)
				}
			}
		}
		if !found {
			t.Fatalf("%s: no google/osv-scanner-action/osv-scanner-action step found", wfPath)
		}
	}
}
