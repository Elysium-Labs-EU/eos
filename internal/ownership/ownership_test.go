package ownership

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestStatOwner_MatchesRealOwner verifies StatOwner reports the same uid/gid
// os.Stat itself resolves for a real file — no root needed, unlike the chown
// half of Align.
func TestStatOwner_MatchesRealOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	uid, gid, err, ok := StatOwner(path)
	if err != nil {
		t.Fatalf("StatOwner: %v", err)
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
		t.Errorf("StatOwner returned uid=%d gid=%d, want uid=%d gid=%d", uid, gid, stat.Uid, stat.Gid)
	}
}

// TestStatOwner_MissingPath verifies StatOwner surfaces a stat error rather
// than silently returning zero values for a path that doesn't exist.
func TestStatOwner_MissingPath(t *testing.T) {
	_, _, err, ok := StatOwner(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
	if !ok {
		t.Error("expected ok=true (a stat error, not a non-POSIX filesystem) for a missing path")
	}
}

// TestChownTolerant_MissingFileIsNotAnError verifies a "not written yet" file
// (e.g. a WAL/SHM sidecar or a not-yet-rotated log) is tolerated. Chowning to
// the caller's own uid/gid doesn't require root, so this exercises the
// tolerance branch without it.
func TestChownTolerant_MissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if err := ChownTolerant(path, os.Getuid(), os.Getgid()); err != nil {
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
	if err := ChownTolerant(path, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("chowning a file to its own current owner should not error: %v", err)
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

// TestAlign_NonRootNoop verifies Align is a harmless no-op when the process
// is not root: no error, and the target files' ownership is left untouched
// (the common, non-sudo code path).
func TestAlign_NonRootNoop(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this case asserts the non-root no-op; skip when running as root")
	}
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "state.db")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	before := ownerUID(t, path)

	if err := Align(baseDir, path); err != nil {
		t.Fatalf("Align returned error on non-root path: %v", err)
	}
	if after := ownerUID(t, path); after != before {
		t.Errorf("non-root call changed ownership: before uid %d, after uid %d", before, after)
	}
}

// TestAlign_RootMatchesDirOwner verifies the fix for issue #14/#91: when
// running as root, files created under baseDir (state.db and its WAL/SHM
// sidecars, daemon.log, eos.pid, ...) are chowned to match the base
// directory's owner, rather than being left root-owned.
func TestAlign_RootMatchesDirOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown files to another uid")
	}
	const targetUID, targetGID = 12345, 12345

	baseDir := t.TempDir()
	if err := os.Chown(baseDir, targetUID, targetGID); err != nil {
		t.Fatalf("chown base dir to target uid: %v", err)
	}

	dbPath := filepath.Join(baseDir, "state.db")
	paths := []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		// Created by root -> owned by root until Align fixes it.
		if got := ownerUID(t, p); got != 0 {
			t.Fatalf("precondition: %s should start root-owned, got uid %d", p, got)
		}
	}

	if err := Align(baseDir, paths...); err != nil {
		t.Fatalf("Align: %v", err)
	}

	for _, p := range paths {
		if got := ownerUID(t, p); got != targetUID {
			t.Errorf("%s: expected owner uid %d (matching base dir), got %d", p, targetUID, got)
		}
	}
}

// TestAlign_MissingPathTolerated verifies Align tolerates a path in the list
// that doesn't exist yet (mirroring ChownTolerant), rather than failing the
// whole alignment because one sidecar/rotated-log hasn't been created.
func TestAlign_MissingPathTolerated(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to exercise the chown branch")
	}
	const targetUID, targetGID = 12345, 12345

	baseDir := t.TempDir()
	if err := os.Chown(baseDir, targetUID, targetGID); err != nil {
		t.Fatalf("chown base dir to target uid: %v", err)
	}

	existing := filepath.Join(baseDir, "daemon.log")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", existing, err)
	}
	missing := filepath.Join(baseDir, "daemon.log.1")

	if err := Align(baseDir, existing, missing); err != nil {
		t.Fatalf("Align with a missing path should not error: %v", err)
	}
	if got := ownerUID(t, existing); got != targetUID {
		t.Errorf("existing path: expected owner uid %d, got %d", targetUID, got)
	}
}
