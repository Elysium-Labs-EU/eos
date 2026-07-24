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

// Identity is the resolved answer to "which user is this process really
// running as." The field is unexported so the only way to obtain a value is
// through ResolveIdentity — callers cannot fake or re-derive one by hand,
// which is what makes this a compiler-enforced guarantee rather than a
// convention (see https://github.com/Elysium-Labs-EU/eos/issues/97).
type Identity struct {
	u   *user.User
	uid uint32
	gid uint32
}

// ResolveIdentity is the only way to obtain an Identity. It wraps
// EffectiveUser and UserCredentials so every call site that needs to know
// "who is this" gets the same, already-correct answer instead of
// reimplementing the SUDO_USER/euid check itself.
func ResolveIdentity() (Identity, error) {
	u, err := EffectiveUser()
	if err != nil {
		return Identity{}, fmt.Errorf("resolving effective user: %w", err)
	}
	uid, gid, err := UserCredentials(u)
	if err != nil {
		return Identity{}, fmt.Errorf("resolving user credentials: %w", err)
	}
	return Identity{u: u, uid: uid, gid: gid}, nil
}

// HomeDir returns the identity's home directory.
func (i Identity) HomeDir() string { return i.u.HomeDir }

// Username returns the identity's username.
func (i Identity) Username() string { return i.u.Username }

// UID returns the identity's numeric uid.
func (i Identity) UID() uint32 { return i.uid }

// GID returns the identity's numeric gid.
func (i Identity) GID() uint32 { return i.gid }
