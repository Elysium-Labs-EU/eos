package database

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestStatOwner_MatchesRealOwner verifies statOwner reports the same uid/gid
// os.Stat itself resolves for a real file — no root needed, unlike the chown
// half of alignDataFileOwnership.
func TestStatOwner_MatchesRealOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	uid, gid, err, ok := statOwner(path)
	if err != nil {
		t.Fatalf("statOwner: %v", err)
	}
	if !ok {
		t.Skip("non-POSIX filesystem; cannot read owner uid/gid")
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("os.Stat: %v", statErr)
	}
	stat, isStatT := info.Sys().(*syscall.Stat_t)
	if !isStatT {
		t.Fatal("expected *syscall.Stat_t on this platform")
	}
	if uid != int(stat.Uid) || gid != int(stat.Gid) {
		t.Errorf("statOwner returned uid=%d gid=%d, want uid=%d gid=%d", uid, gid, stat.Uid, stat.Gid)
	}
}

// TestStatOwner_MissingPath verifies statOwner surfaces a stat error rather
// than silently returning zero values for a path that doesn't exist.
func TestStatOwner_MissingPath(t *testing.T) {
	_, _, err, ok := statOwner(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
	if !ok {
		t.Error("expected ok=true (a stat error, not a non-POSIX filesystem) for a missing path")
	}
}

// TestChownTolerant_MissingFileIsNotAnError verifies the WAL/SHM sidecars'
// "not written yet" case is tolerated. Chowning to the caller's own uid/gid
// doesn't require root, so this exercises the tolerance branch without it.
func TestChownTolerant_MissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if err := chownTolerant(path, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("expected a missing file to be tolerated, got: %v", err)
	}
}

// TestChownTolerant_RealFileNoop verifies chowning an existing file to its
// own current owner succeeds without requiring root.
func TestChownTolerant_RealFileNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	if err := chownTolerant(path, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("chowning a file to its own current owner should not error: %v", err)
	}
}

// TestNewDB_Success exercises NewDB end-to-end (open, migrate, ownership
// alignment) on a fresh base dir — every other database test goes through
// NewTestDB instead, so without this NewDB itself has zero direct coverage.
func TestNewDB_Success(t *testing.T) {
	db, err := NewDB(t.Context(), t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.CloseDBConnection() })

	if _, err := db.GetAllServiceInstances(t.Context()); err != nil {
		t.Errorf("querying a freshly migrated DB: %v", err)
	}
}

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
