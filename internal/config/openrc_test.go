package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsOpenRCManaged(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		managed, err := IsOpenRCManaged(t.TempDir(), OpenRCTargetFileName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if managed {
			t.Error("expected not managed when init script is absent")
		}
	})

	t.Run("installed", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, OpenRCTargetFileName), []byte("#!/sbin/openrc-run\n"), 0755); err != nil {
			t.Fatalf("writing init script: %v", err)
		}
		managed, err := IsOpenRCManaged(dir, OpenRCTargetFileName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !managed {
			t.Error("expected managed when init script exists")
		}
	})
}

func TestIsUnderOpenRC_NotSupervised(t *testing.T) {
	// The test binary's parent is `go test`, never supervise-daemon, so this must
	// report false. The true case is verified end-to-end on the real Alpine VM.
	if IsUnderOpenRC() {
		t.Error("expected IsUnderOpenRC to be false when not run by supervise-daemon")
	}
}
