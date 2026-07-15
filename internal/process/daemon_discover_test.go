package process

import (
	"os"
	"path/filepath"
	"runtime"
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
