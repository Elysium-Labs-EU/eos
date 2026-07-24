package main

import (
	"os"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// TestDependencyMinimumVersions guards against re-introducing known CVEs
// (issue #57 / OSV-flagged GHSA-hrxh-6v49-42gf, GO-2026-5942, GO-2026-5970)
// by regressing these indirect deps below their fixed versions.
func TestDependencyMinimumVersions(t *testing.T) {
	minVersions := map[string]string{
		"google.golang.org/grpc": "v1.82.1",
		"golang.org/x/net":       "v0.56.0",
		"golang.org/x/text":      "v0.39.0",
	}

	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("parsing go.mod: %v", err)
	}

	found := make(map[string]string)
	for _, req := range f.Require {
		if min, ok := minVersions[req.Mod.Path]; ok {
			found[req.Mod.Path] = req.Mod.Version
			if semver.Compare(req.Mod.Version, min) < 0 {
				t.Errorf("%s at %s is below fixed version %s", req.Mod.Path, req.Mod.Version, min)
			}
		}
	}
	for path := range minVersions {
		if _, ok := found[path]; !ok {
			t.Errorf("%s missing from go.mod require block", path)
		}
	}
}
