package buildinfo

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	origVersion, origCommit, origDate := Version, GitCommit, BuildDate
	t.Cleanup(func() { Version, GitCommit, BuildDate = origVersion, origCommit, origDate })

	Version = "1.2.3"
	GitCommit = "abc123"
	BuildDate = "2026-01-01"

	got := Get()
	for _, want := range []string{"1.2.3", "abc123", "2026-01-01"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected Get() output to contain %q, got: %q", want, got)
		}
	}
}

func TestGetVersionOnly(t *testing.T) {
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })

	Version = "9.9.9"
	if got := GetVersionOnly(); got != "9.9.9" {
		t.Errorf("expected %q, got %q", "9.9.9", got)
	}
}

func TestGet_Defaults(t *testing.T) {
	// dev/unknown are the values baked in when no linker flags are passed
	// (e.g. `go test`, `go run` without -ldflags), not just placeholders.
	if Version != "dev" {
		t.Skip("Version overridden by linker flags in this build")
	}
	got := Get()
	if !strings.Contains(got, "dev") || !strings.Contains(got, "unknown") {
		t.Errorf("expected default dev/unknown values in output, got: %q", got)
	}
}
