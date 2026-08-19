//go:build integration

package testmatrix

import (
	"context"
	"strings"
	"testing"
)

// TestOrb_RealSmoke exercises Orb against a real OrbStack machine: clone the
// existing "alpine" VM, run a trivial command in the clone, then delete it.
// Requires OrbStack running locally with an "alpine" machine present.
func TestOrb_RealSmoke(t *testing.T) {
	o := NewOrb()
	ctx := context.Background()
	clone := "eos-testmatrix-smoke"

	t.Cleanup(func() {
		_ = o.Delete(context.Background(), clone)
	})

	if err := o.Clone(ctx, "alpine", clone); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	out, err := o.Run(ctx, clone, "uname -a")
	if err != nil {
		t.Fatalf("Run: %v (output: %s)", err, out)
	}
	if !strings.Contains(strings.ToLower(out), "linux") {
		t.Fatalf("expected uname output to mention linux, got %q", out)
	}

	if err := o.Delete(ctx, clone); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
