package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverDaemonsIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relies on /proc/<pid>/exe, linux only")
	}

	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	selfIno := inodeOf(selfExe)
	if selfIno == 0 {
		t.Fatal("could not resolve inode of own executable")
	}

	root := t.TempDir()

	// user "alice" has no .eos dir at all — should be skipped entirely.
	aliceHome := filepath.Join(root, "alice")
	if err := os.MkdirAll(aliceHome, 0755); err != nil {
		t.Fatal(err)
	}

	// user "bob" has a .eos dir but no pid file — not running.
	bobHome := filepath.Join(root, "bob")
	if err := os.MkdirAll(filepath.Join(bobHome, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}

	// user "carol" has a live daemon running the current binary — not stale.
	carolHome := filepath.Join(root, "carol")
	if err := os.MkdirAll(filepath.Join(carolHome, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}
	writePIDFile(t, filepath.Join(carolHome, ".eos", "eos.pid"), os.Getpid())

	homeDirs := []string{aliceHome, bobHome, carolHome}

	summaries := discoverDaemonsIn(homeDirs, selfIno)

	// "dave" runs the same real PID (this test process, via /proc/<pid>/exe) but is
	// checked against a deliberately wrong currentIno — exercises the mismatch branch,
	// since we can't fake /proc/<pid>/exe to point at a genuinely different binary.
	daveHome := filepath.Join(root, "dave")
	if err := os.MkdirAll(filepath.Join(daveHome, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}
	writePIDFile(t, filepath.Join(daveHome, ".eos", "eos.pid"), os.Getpid())
	daveSummaries := discoverDaemonsIn([]string{daveHome}, selfIno+1)
	summaries = append(summaries, daveSummaries...)

	byUser := make(map[string]DaemonSummary, len(summaries))
	for _, s := range summaries {
		byUser[s.Username] = s
	}

	if _, ok := byUser["alice"]; ok {
		t.Error("alice has no .eos dir and should have been skipped")
	}

	bob, ok := byUser["bob"]
	if !ok {
		t.Fatal("expected a summary for bob")
	}
	if bob.Status == nil || bob.Status.Running {
		t.Errorf("bob has no pid file, expected not running, got %+v", bob.Status)
	}

	carol, ok := byUser["carol"]
	if !ok {
		t.Fatal("expected a summary for carol")
	}
	if carol.Status == nil || !carol.Status.Running {
		t.Fatalf("carol should be running, got %+v", carol.Status)
		return
	}
	if carol.StaleBinary {
		t.Error("carol is running the current binary, should not be flagged stale")
	}

	dave, ok := byUser["dave"]
	if !ok {
		t.Fatal("expected a summary for dave")
	}
	if dave.Status == nil || !dave.Status.Running {
		t.Fatalf("dave should be running, got %+v", dave.Status)
	}
	if !dave.StaleBinary {
		t.Error("dave was checked against a mismatched inode, expected stale")
	}

	// sorted by username
	var usernames []string
	for _, s := range summaries {
		usernames = append(usernames, s.Username)
	}
	for i := 1; i < len(usernames); i++ {
		if usernames[i-1] > usernames[i] {
			t.Errorf("summaries not sorted by username: %v", usernames)
			break
		}
	}
}

func TestDiscoverDaemonsInZeroCurrentIno(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relies on /proc/<pid>/exe, linux only")
	}

	root := t.TempDir()
	home := filepath.Join(root, "erin")
	if err := os.MkdirAll(filepath.Join(home, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}
	writePIDFile(t, filepath.Join(home, ".eos", "eos.pid"), os.Getpid())

	summaries := discoverDaemonsIn([]string{home}, 0)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].StaleBinary {
		t.Error("currentIno=0 means unknown, should never flag stale")
	}
}

func TestDiscoverDaemonsInCorruptPIDFile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "frank")
	if err := os.MkdirAll(filepath.Join(home, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".eos", "eos.pid"), []byte("not-a-pid"), 0600); err != nil {
		t.Fatal(err)
	}

	summaries := discoverDaemonsIn([]string{home}, 0)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Err == nil {
		t.Error("expected an error for an unparsable pid file")
	}
	if summaries[0].Status != nil {
		t.Errorf("status should be nil when the pid file couldn't be parsed, got %+v", summaries[0].Status)
	}
}

func TestDiscoverDaemonsInDeadPID(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "grace")
	if err := os.MkdirAll(filepath.Join(home, ".eos"), 0755); err != nil {
		t.Fatal(err)
	}
	// A pid astronomically unlikely to be alive on the test host.
	writePIDFile(t, filepath.Join(home, ".eos", "eos.pid"), 1<<30)

	summaries := discoverDaemonsIn([]string{home}, 0)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Err != nil {
		t.Errorf("a dead pid should not be an error, got: %v", summaries[0].Err)
	}
	if summaries[0].Status == nil || summaries[0].Status.Running {
		t.Errorf("expected not running for a dead pid, got %+v", summaries[0].Status)
	}
	if summaries[0].StaleBinary {
		t.Error("a non-running daemon should never be flagged stale")
	}
}

func TestInodeOf(t *testing.T) {
	t.Run("existing file returns a nonzero inode", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := inodeOf(f); got == 0 {
			t.Error("expected a nonzero inode for an existing file")
		}
	})

	t.Run("missing file returns zero", func(t *testing.T) {
		if got := inodeOf(filepath.Join(t.TempDir(), "does-not-exist")); got != 0 {
			t.Errorf("expected 0 for a missing file, got %d", got)
		}
	})
}

func TestCurrentExecutableInode(t *testing.T) {
	if got := CurrentExecutableInode(); got == 0 {
		t.Error("expected a nonzero inode for the running test binary")
	}
}

// TestRunningExeInode exercises both branches of the /proc/<pid>/exe lookup
// across platforms: on linux the magic symlink resolves for a real pid, and
// on darwin (no /proc) the underlying os.Stat fails, hitting inodeOf's own
// error return.
func TestRunningExeInode(t *testing.T) {
	got := RunningExeInode(os.Getpid())
	if runtime.GOOS == "linux" {
		if got == 0 {
			t.Error("expected a nonzero inode via /proc/<pid>/exe on linux")
		}
		return
	}
	if got != 0 {
		t.Errorf("expected 0 without /proc on %s, got %d", runtime.GOOS, got)
	}
}

// TestReadHomeDirs proves readHomeDirs never errors on a missing /home (the
// common case off Linux) and returns whatever entries exist otherwise -
// /home's actual contents vary by host, so this only asserts the no-panic,
// no-spurious-error contract rather than a specific directory list.
func TestReadHomeDirs(t *testing.T) {
	dirs, err := readHomeDirs()
	if err != nil {
		t.Fatalf("readHomeDirs: %v", err)
	}
	for _, d := range dirs {
		if filepath.Dir(d) != "/home" {
			t.Errorf("expected entry rooted at /home, got %q", d)
		}
	}
}

func TestCandidateHomeDirs(t *testing.T) {
	dirs, err := candidateHomeDirs()
	if err != nil {
		t.Fatalf("candidateHomeDirs: %v", err)
	}
	if _, statErr := os.Stat("/root"); statErr == nil {
		found := false
		for _, d := range dirs {
			if d == "/root" {
				found = true
			}
		}
		if !found {
			t.Error("expected /root among candidate home dirs when it exists")
		}
	}
}

// TestDiscoverDaemons exercises the exported entry point: it's linux-only by
// contract, so anywhere else it must fail loud with an actionable error
// rather than silently scanning the wrong filesystem layout.
func TestDiscoverDaemons(t *testing.T) {
	if runtime.GOOS != "linux" {
		_, err := DiscoverDaemons()
		if err == nil {
			t.Fatal("expected an error on non-linux platforms")
		}
		if !strings.Contains(err.Error(), "linux") {
			t.Errorf("expected a linux-only error, got: %v", err)
		}
		return
	}
	if _, err := DiscoverDaemons(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
