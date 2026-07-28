//go:build !linux && !darwin

package process

import (
	"fmt"
	"net"
	"runtime"
)

// peerUID has no implementation outside Linux and macOS, the two platforms
// eos supports. Callers must treat this error as a hard failure rather than
// skipping the check, which would reopen the control socket to any local
// caller on this platform.
func peerUID(conn net.Conn) (uint32, error) {
	return 0, fmt.Errorf("peer credential lookup not supported on %s", runtime.GOOS)
}
