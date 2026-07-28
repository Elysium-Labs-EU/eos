// Package ownership aligns files created while effectively root (e.g. during
// a sudo-invoked daemon (re)start) with the owner of eos's base directory, so
// they stay writable to the unprivileged user that invokes eos day to day.
package ownership

import (
	"fmt"
	"os"
	"syscall"
)

// StatOwner reports the uid/gid that own path, or ok=false on a non-POSIX
// filesystem where that information isn't available.
func StatOwner(path string) (uid, gid int, err error, ok bool) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return 0, 0, fmt.Errorf("stat %s for ownership: %w", path, statErr), true
	}
	stat, isStatT := info.Sys().(*syscall.Stat_t)
	if !isStatT {
		return 0, 0, nil, false
	}
	return int(stat.Uid), int(stat.Gid), nil, true
}

// ChownTolerant chowns path to uid/gid, treating a missing file as success —
// callers often chown paths (e.g. WAL/SHM sidecars, rotated logs) that aren't
// guaranteed to exist yet.
func ChownTolerant(path string, uid, gid int) error {
	if err := os.Chown(path, uid, gid); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("chown %s to uid %d gid %d: %w", path, uid, gid, err)
	}
	return nil
}

// ValidateTrusted checks that path is safe for eos to treat as a trust
// boundary (e.g. an EOS_BASE_DIR override): it must not be group- or
// world-writable, and must be owned by uid. When runningAsRoot is true, a
// path still owned by root itself is also accepted — root is trusted to
// reassign ownership to uid next (see config.CreateBaseDir's chown step), so
// refusing it here would only break that self-healing, not stop an attacker.
// A directory owned by any other uid, or that is group/world-writable, may
// have been staged by an untrusted party and is refused.
//
// A path that does not exist yet is not a trust boundary and passes: the
// caller is responsible for creating it with a safe owner/mode.
func ValidateTrusted(path string, uid uint32, runningAsRoot bool) error {
	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return nil
	}
	if statErr != nil {
		return fmt.Errorf("stat %s for trust validation: %w", path, statErr)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group- or world-writable (mode %o); refusing to trust it", path, info.Mode().Perm())
	}
	stat, isStatT := info.Sys().(*syscall.Stat_t)
	if !isStatT {
		return nil
	}
	if stat.Uid == uid {
		return nil
	}
	if runningAsRoot && stat.Uid == 0 {
		return nil
	}
	return fmt.Errorf("%s is owned by uid %d, expected uid %d; refusing to trust it", path, stat.Uid, uid)
}

// Align chowns each of paths to match baseDir's owner. Under sudo (euid 0) the
// daemon's own file creation (state.db, daemon.log, eos.pid, ...) happens as
// root even though the base directory itself was already chowned to the
// invoking user (see config.CreateBaseDir); that mismatch leaves those files
// unwritable to the later User=<invoker> systemd daemon and any unprivileged
// CLI. See issue #14 (state.db) and issue #91 (daemon.log, eos.pid).
//
// The invoking user is derived from baseDir's owner rather than re-resolving
// the sudo identity, so the whole base dir stays internally consistent and
// files created before this fix self-heal on the next root invocation. Align
// is a no-op when not running as root (the common non-sudo path) or on
// non-POSIX systems.
func Align(baseDir string, paths ...string) error {
	if os.Geteuid() != 0 {
		return nil
	}

	uid, gid, err, ok := StatOwner(baseDir)
	if err != nil || !ok {
		return err
	}

	for _, path := range paths {
		if err := ChownTolerant(path, uid, gid); err != nil {
			return err
		}
	}
	return nil
}
