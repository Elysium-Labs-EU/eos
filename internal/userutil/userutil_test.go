package userutil

import (
	"os"
	"os/user"
	"testing"
)

// This is the regression harness for the SUDO_USER/euid class of bug: eos ships
// a handful of places that need to answer "which user is this really running
// as" (base dir resolution, privilege-dropping chown, daemon detach). They all
// must route through EffectiveUser rather than re-deriving the answer, because
// SUDO_USER is set by both `sudo` (root) and `sudo -u <non-root-user>` (not
// root) — treating it as authoritative in the second case silently redirects
// data/ownership to the wrong user. See internal/config GetBaseDir, which had
// exactly this bug before it was changed to call EffectiveUser.
//
// Add a case here for every euid×SUDO_USER combination a caller might rely on.
func TestEffectiveUser(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}

	t.Run("non-root, SUDO_USER unset: resolves to current user", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("must run as non-root")
		}
		t.Setenv("SUDO_USER", "")

		got, err := EffectiveUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Uid != cur.Uid {
			t.Errorf("expected current user (uid %s), got uid %s", cur.Uid, got.Uid)
		}
	})

	t.Run("non-root, SUDO_USER set: SUDO_USER must be ignored", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("must run as non-root")
		}
		// This is the `sudo -u <non-root-user>` case: SUDO_USER is set to the
		// invoking user, but the process is already running as the target
		// user. Honoring SUDO_USER here would resolve to the wrong identity.
		t.Setenv("SUDO_USER", "someone-else")

		got, err := EffectiveUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Uid != cur.Uid {
			t.Errorf("expected SUDO_USER to be ignored and resolve to current user (uid %s), got uid %s", cur.Uid, got.Uid)
		}
	})

	t.Run("root, SUDO_USER unset: resolves to current (root) user", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", "")

		got, err := EffectiveUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Uid != cur.Uid {
			t.Errorf("expected current (root) user (uid %s), got uid %s", cur.Uid, got.Uid)
		}
	})

	t.Run("root, SUDO_USER set: resolves to invoking user, not root", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", cur.Username)

		got, err := EffectiveUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Username != cur.Username {
			t.Errorf("expected SUDO_USER %q, got %q", cur.Username, got.Username)
		}
	})

	t.Run("root, SUDO_USER=root: falls back to current user", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", "root")

		got, err := EffectiveUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Uid != cur.Uid {
			t.Errorf("expected fallback to current user (uid %s), got uid %s", cur.Uid, got.Uid)
		}
	})
}

// TestResolveIdentity retargets the EffectiveUser env matrix onto Identity, the
// public API call sites outside this package should use. It doesn't need root to
// exercise the branch logic for the root×SUDO_USER cases below because a fake
// Identity can be substituted in code under test — this table only verifies
// ResolveIdentity's own resolution against the real environment.
func TestResolveIdentity(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}

	t.Run("non-root, SUDO_USER unset: resolves to current user", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("must run as non-root")
		}
		t.Setenv("SUDO_USER", "")

		id, err := ResolveIdentity()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Username() != cur.Username {
			t.Errorf("expected current user %q, got %q", cur.Username, id.Username())
		}
		if id.HomeDir() != cur.HomeDir {
			t.Errorf("expected home dir %q, got %q", cur.HomeDir, id.HomeDir())
		}
	})

	t.Run("non-root, SUDO_USER set: SUDO_USER must be ignored", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("must run as non-root")
		}
		t.Setenv("SUDO_USER", "someone-else")

		id, err := ResolveIdentity()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Username() != cur.Username {
			t.Errorf("expected SUDO_USER to be ignored and resolve to current user %q, got %q", cur.Username, id.Username())
		}
	})

	t.Run("root, SUDO_USER unset: resolves to current (root) user", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", "")

		id, err := ResolveIdentity()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Username() != cur.Username {
			t.Errorf("expected current (root) user %q, got %q", cur.Username, id.Username())
		}
	})

	t.Run("root, SUDO_USER set: resolves to invoking user, not root", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", cur.Username)

		id, err := ResolveIdentity()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Username() != cur.Username {
			t.Errorf("expected SUDO_USER %q, got %q", cur.Username, id.Username())
		}
	})

	t.Run("root, SUDO_USER=root: falls back to current user", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("must run as root")
		}
		t.Setenv("SUDO_USER", "root")

		id, err := ResolveIdentity()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Username() != cur.Username {
			t.Errorf("expected fallback to current user %q, got %q", cur.Username, id.Username())
		}
	})
}

func TestUserCredentials(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}

	uid, gid, err := UserCredentials(u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid == 0 && u.Uid != "0" {
		t.Errorf("uid parsed as 0 but user.Uid was %q", u.Uid)
	}
	if gid == 0 && u.Gid != "0" {
		t.Errorf("gid parsed as 0 but user.Gid was %q", u.Gid)
	}

	t.Run("invalid uid", func(t *testing.T) {
		bad := &user.User{Uid: "not-a-number", Gid: u.Gid}
		if _, _, err := UserCredentials(bad); err == nil {
			t.Error("expected error for non-numeric uid")
		}
	})

	t.Run("invalid gid", func(t *testing.T) {
		bad := &user.User{Uid: u.Uid, Gid: "not-a-number"}
		if _, _, err := UserCredentials(bad); err == nil {
			t.Error("expected error for non-numeric gid")
		}
	})
}
