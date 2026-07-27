package process

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

// reusePortServerEnv, when set, turns a re-exec of the test binary into the
// SO_REUSEPORT listener process the reload OS-level test supervises, instead of
// a normal test run. TestReloadZeroDowntimeSOReusePort sets it on the service
// command it registers so eos launches this same binary as the managed service.
const reusePortServerEnv = "EOS_TEST_REUSEPORT_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(reusePortServerEnv) != "" {
		os.Exit(runReusePortServer())
	}
	goleak.VerifyTestMain(m)
}
