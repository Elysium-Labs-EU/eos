// Package userutil provides helpers for resolving the effective OS user,
// accounting for sudo invocations.
package userutil

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// EffectiveUser returns the non-root user who invoked sudo, falling back to the current user.
// Use this when a process needs to run as the invoking user rather than root.
func EffectiveUser() (*user.User, error) {
	if os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
			return user.Lookup(sudoUser)
		}
	}
	return user.Current()
}

// UserCredentials returns the uid and gid for a user as uint32 values suitable for syscall.Credential.
func UserCredentials(u *user.User) (uid uint32, gid uint32, err error) {
	uidInt, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing uid: %w", err)
	}
	gidInt, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing gid: %w", err)
	}
	return uint32(uidInt), uint32(gidInt), nil
}
