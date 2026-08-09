// install_test.go exercises install.sh's release-version resolution — the
// piece of the installer that shipped issue #43. It shells out to the real
// script (sourced with EOS_INSTALL_SOURCE_ONLY=1, which skips running the
// installer itself and only defines its functions) rather than
// reimplementing the shell logic in Go.
package main_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runInstallFunc(t *testing.T, script string, env map[string]string) (string, error) {
	t.Helper()

	cmd := exec.Command("bash", "-c", "source install.sh; "+script)
	cmd.Env = append(os.Environ(), "EOS_INSTALL_SOURCE_ONLY=1")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestExtractTagName(t *testing.T) {
	out, err := runInstallFunc(t, `printf '%s' "$FIXTURE" | extract_tag_name`, map[string]string{
		"FIXTURE": `{"tag_name": "v0.0.12", "prerelease": false}`,
	})
	if err != nil {
		t.Fatalf("extract_tag_name failed: %v\n%s", err, out)
	}
	if out != "v0.0.12" {
		t.Errorf("extract_tag_name = %q, want %q", out, "v0.0.12")
	}
}

func TestPickLatestTagOutOfOrderList(t *testing.T) {
	// Reproduces issue #43: the Codeberg (Gitea) /releases list is
	// documented newest-first but was observed live to return a release
	// out of order — a release published minutes earlier sorted 3rd, not
	// 1st. Trusting list position (as the old releases[0] logic did)
	// would silently pick the stale v0.0.9-rc.1... or here, the stale
	// v0.0.12-rc.4 instead of the actually-newest v0.0.12-rc.5.
	out, err := runInstallFunc(t, `printf '%s' "$FIXTURE" | pick_latest_tag`, map[string]string{
		"FIXTURE": `[
			{"tag_name": "v0.0.9-rc.1", "prerelease": true},
			{"tag_name": "v0.0.12-rc.5", "prerelease": true},
			{"tag_name": "v0.0.12-rc.4", "prerelease": true}
		]`,
	})
	if err != nil {
		t.Fatalf("pick_latest_tag failed: %v\n%s", err, out)
	}
	if out != "v0.0.12-rc.5" {
		t.Errorf("pick_latest_tag = %q, want the highest by semver %q", out, "v0.0.12-rc.5")
	}
}

func TestPickLatestTagPrefersStableOverNewerPrerelease(t *testing.T) {
	out, err := runInstallFunc(t, `printf '%s' "$FIXTURE" | pick_latest_tag`, map[string]string{
		"FIXTURE": `[
			{"tag_name": "v0.2.0-rc.1", "prerelease": true},
			{"tag_name": "v0.1.0", "prerelease": false}
		]`,
	})
	if err != nil {
		t.Fatalf("pick_latest_tag failed: %v\n%s", err, out)
	}
	if out != "v0.1.0" {
		t.Errorf("pick_latest_tag = %q, want the stable release %q over the newer prerelease", out, "v0.1.0")
	}
}

func TestPickLatestTagAllPrereleaseFallsBackToHighest(t *testing.T) {
	out, err := runInstallFunc(t, `printf '%s' "$FIXTURE" | pick_latest_tag`, map[string]string{
		"FIXTURE": `[
			{"tag_name": "v0.1.0-rc.1", "prerelease": true},
			{"tag_name": "v0.1.0-rc.2", "prerelease": true}
		]`,
	})
	if err != nil {
		t.Fatalf("pick_latest_tag failed: %v\n%s", err, out)
	}
	if out != "v0.1.0-rc.2" {
		t.Errorf("pick_latest_tag = %q, want the highest prerelease %q", out, "v0.1.0-rc.2")
	}
}

func TestFetchLatestVersionUsesReleasesLatestEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v0.0.11", "prerelease": false}`))
	}))
	defer srv.Close()

	out, err := runInstallFunc(t, `fetch_latest_version curl`, map[string]string{"EOS_API_BASE": srv.URL})
	if err != nil {
		t.Fatalf("fetch_latest_version failed: %v\n%s", err, out)
	}
	if out != "v0.0.11" {
		t.Errorf("fetch_latest_version = %q, want %q", out, "v0.0.11")
	}
}

func TestFetchLatestVersionOutOfOrderListFallback(t *testing.T) {
	// /releases/latest 404s (every release so far is a prerelease); the
	// fallback list is out of order the same way as issue #43 — the
	// newest tag isn't first — and fetch_latest_version must still pick
	// it by semver, not list position.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name": "v0.0.9-rc.1", "prerelease": true},
			{"tag_name": "v0.0.12-rc.5", "prerelease": true},
			{"tag_name": "v0.0.12-rc.4", "prerelease": true}
		]`))
	}))
	defer srv.Close()

	out, err := runInstallFunc(t, `fetch_latest_version curl`, map[string]string{"EOS_API_BASE": srv.URL})
	if err != nil {
		t.Fatalf("fetch_latest_version failed: %v\n%s", err, out)
	}
	if out != "v0.0.12-rc.5" {
		t.Errorf("fetch_latest_version = %q, want the highest by semver %q", out, "v0.0.12-rc.5")
	}
}

func TestFetchLatestVersionFallsBackToStableOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name": "v0.2.0-rc.1", "prerelease": true},
			{"tag_name": "v0.1.0", "prerelease": false}
		]`))
	}))
	defer srv.Close()

	out, err := runInstallFunc(t, `fetch_latest_version curl`, map[string]string{"EOS_API_BASE": srv.URL})
	if err != nil {
		t.Fatalf("fetch_latest_version failed: %v\n%s", err, out)
	}
	if out != "v0.1.0" {
		t.Errorf("fetch_latest_version = %q, want the stable release %q over the newer prerelease", out, "v0.1.0")
	}
}

func TestFetchLatestVersionAllPrereleaseFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name": "v0.1.0-rc.1", "prerelease": true},
			{"tag_name": "v0.1.0-rc.2", "prerelease": true}
		]`))
	}))
	defer srv.Close()

	out, err := runInstallFunc(t, `fetch_latest_version curl`, map[string]string{"EOS_API_BASE": srv.URL})
	if err != nil {
		t.Fatalf("fetch_latest_version failed: %v\n%s", err, out)
	}
	if out != "v0.1.0-rc.2" {
		t.Errorf("fetch_latest_version = %q, want the highest prerelease %q", out, "v0.1.0-rc.2")
	}
}

// TestInstallOpensslViaPackageManagers reproduces issue #242: Fedora and
// Alpine don't ship openssl by default, so the signature-verification step
// must install it through the detected package manager instead of refusing
// outright. Each package manager's install command is stubbed as a shell
// function so no real package installs happen.
func TestInstallOpensslViaPackageManagers(t *testing.T) {
	cases := []struct {
		pkgManager string
		stubCmd    string
		wantMsg    string
	}{
		{"apt", "apt-get", "openssl installed via apt"},
		{"yum", "yum", "openssl installed via yum"},
		{"dnf", "dnf", "openssl installed via dnf"},
		{"apk", "apk", "openssl installed via apk"},
		{"pacman", "pacman", "openssl installed via pacman"},
	}

	for _, tc := range cases {
		t.Run(tc.pkgManager, func(t *testing.T) {
			script := tc.stubCmd + `() { return 0; }; install_openssl ` + tc.pkgManager
			out, err := runInstallFunc(t, script, nil)
			if err != nil {
				t.Fatalf("install_openssl %s failed: %v\n%s", tc.pkgManager, err, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("install_openssl %s output = %q, want substring %q", tc.pkgManager, out, tc.wantMsg)
			}
		})
	}
}

func TestInstallOpensslPropagatesPackageManagerFailure(t *testing.T) {
	out, err := runInstallFunc(t, `apt-get() { return 1; }; install_openssl apt`, nil)
	if err == nil {
		t.Fatalf("install_openssl apt (mock failure) = success, want failure\n%s", out)
	}
	if !strings.Contains(out, "Failed to install openssl") {
		t.Errorf("install_openssl apt (mock failure) output = %q, want the failure message", out)
	}
}

func TestInstallOpensslBrewRefusesUnderSudo(t *testing.T) {
	out, err := runInstallFunc(t, `install_openssl brew`, nil)
	if err == nil {
		t.Fatalf("install_openssl brew = success, want failure (Homebrew refuses to run as root)\n%s", out)
	}
	if !strings.Contains(out, "Homebrew refuses to run as root") {
		t.Errorf("install_openssl brew output = %q, want the root-refusal message", out)
	}
}

func TestInstallOpensslUnknownPackageManager(t *testing.T) {
	out, err := runInstallFunc(t, `install_openssl unknown`, nil)
	if err == nil {
		t.Fatalf("install_openssl unknown = success, want failure\n%s", out)
	}
	if !strings.Contains(out, "Could not detect package manager") {
		t.Errorf("install_openssl unknown output = %q, want the manual-install message", out)
	}
}
