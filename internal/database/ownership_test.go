package database

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// ownerUID returns the owning uid of path, or fails the test.
func ownerUID(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("non-POSIX filesystem; cannot read owner uid")
	}
	return int(stat.Uid)
}

// TestAlignDataFileOwnership_NonRootNoop verifies the helper is a harmless no-op
// when the process is not root: no error, and the db file's ownership is left
// untouched (the common, non-sudo code path).
func TestAlignDataFileOwnership_NonRootNoop(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this case asserts the non-root no-op; skip when running as root")
	}
	baseDir := t.TempDir()
	dbPath := filepath.Join(baseDir, "state.db")
	if err := os.WriteFile(dbPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing db file: %v", err)
	}
	before := ownerUID(t, dbPath)

	if err := alignDataFileOwnership(baseDir, dbPath); err != nil {
		t.Fatalf("alignDataFileOwnership returned error on non-root path: %v", err)
	}
	if after := ownerUID(t, dbPath); after != before {
		t.Errorf("non-root call changed ownership: before uid %d, after uid %d", before, after)
	}
}

// TestAlignDataFileOwnership_RootMatchesDirOwner verifies the fix for issue #14:
// when running as root, state.db (and its WAL/SHM sidecars) are chowned to match
// the base directory's owner, rather than being left root-owned.
func TestAlignDataFileOwnership_RootMatchesDirOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown files to another uid")
	}
	const targetUID, targetGID = 12345, 12345

	baseDir := t.TempDir()
	if err := os.Chown(baseDir, targetUID, targetGID); err != nil {
		t.Fatalf("chown base dir to target uid: %v", err)
	}

	dbPath := filepath.Join(baseDir, "state.db")
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		// Created by root -> owned by root until the helper fixes it.
		if got := ownerUID(t, p); got != 0 {
			t.Fatalf("precondition: %s should start root-owned, got uid %d", p, got)
		}
	}

	if err := alignDataFileOwnership(baseDir, dbPath); err != nil {
		t.Fatalf("alignDataFileOwnership: %v", err)
	}

	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if got := ownerUID(t, p); got != targetUID {
			t.Errorf("%s: expected owner uid %d (matching base dir), got %d", p, targetUID, got)
		}
	}
}
