//go:build darwin

package process

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID returns the UID of the process on the other end of a Unix domain
// socket connection via LOCAL_PEERCRED, the kernel-verified peer credential
// (not spoofable by the client) that lets the daemon tell a legitimate local
// caller from any other local user connecting to the same socket path.
func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("connection is not a unix socket: %T", conn)
	}

	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("getting raw connection: %w", err)
	}

	var xucred *unix.Xucred
	var sockoptErr error
	if ctrlErr := rawConn.Control(func(fd uintptr) {
		xucred, sockoptErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); ctrlErr != nil {
		return 0, fmt.Errorf("accessing socket fd: %w", ctrlErr)
	}
	if sockoptErr != nil {
		return 0, fmt.Errorf("LOCAL_PEERCRED: %w", sockoptErr)
	}
	if xucred == nil {
		return 0, fmt.Errorf("LOCAL_PEERCRED: no credentials returned")
	}

	return xucred.Uid, nil
}
