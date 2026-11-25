package cmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	answerVersionProbe()
	goleak.VerifyTestMain(m, goleak.Cleanup(func(exitCode int) {
		cleanupTestDaemonTempDirs()
		os.Exit(exitCode)
	}))
}

// answerVersionProbe mirrors rootCmd's --version output when runningExeVersion
// execs this test binary as /proc/<pid>/exe --version, so tests can probe
// os.Getpid() instead of a coreutils child (uutils, Ubuntu 26.04+, is a
// multicall binary that refuses to run as `exe`). `go test` always passes
// -test.* flags, so a bare --version only ever comes from that probe. Under
// -cover the probe inherits GOCOVERDIR and writes a counter file on exit,
// which the parent merges; that is harmless.
func answerVersionProbe() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Get())
		os.Exit(0)
	}
}
