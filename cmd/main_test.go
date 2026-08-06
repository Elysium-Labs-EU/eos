package cmd

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.Cleanup(func(exitCode int) {
		cleanupTestDaemonTempDirs()
		os.Exit(exitCode)
	}))
}
