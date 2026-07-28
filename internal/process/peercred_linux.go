//go:build linux

package process

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID returns the UID of the process on the other end of a Unix domain
// socket connection via SO_PEERCRED, the kernel-verified peer credential
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

	var ucred *unix.Ucred
	var sockoptErr error
	if ctrlErr := rawConn.Control(func(fd uintptr) {
		ucred, sockoptErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); ctrlErr != nil {
		return 0, fmt.Errorf("accessing socket fd: %w", ctrlErr)
	}
	if sockoptErr != nil {
		return 0, fmt.Errorf("SO_PEERCRED: %w", sockoptErr)
	}
	if ucred == nil {
		return 0, fmt.Errorf("SO_PEERCRED: no credentials returned")
	}

	return ucred.Uid, nil
}
