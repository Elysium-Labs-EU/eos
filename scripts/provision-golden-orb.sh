#!/usr/bin/env bash
# Create (or refresh) one golden OrbStack VM for the e2e test matrix
# (see test/matrix.yml and internal/testmatrix). A golden VM is a clean,
# long-lived base image: install the Go toolchain once, snapshot nothing
# further -- test/matrix.yml's target.golden is the machine name matrix
# runs clone from.
#
# Installs what the *non-nightly* suites actually need: go toolchain, a C
# compiler (`go test -race` requires cgo), and node -- internal/monitor's
# own test suite spawns real node processes as service fixtures, so `go
# test ./cmd ./internal/...` needs node even outside the nightly
# real-fixture suite. bun/pnpm/jq are still left to the nightly "fixtures"
# suite's own on-demand install (see scripts/test-fixtures-orb.sh) since
# only that suite (real Next.js/Vite/etc fixtures) touches them.
#
# Usage: scripts/provision-golden-orb.sh DISTRO:VERSION NAME
# Example: scripts/provision-golden-orb.sh debian:bookworm eos-golden-debian
#
# Safe to re-run against an existing golden to refresh the Go toolchain
# version -- it reinstalls in place rather than recreating the VM.
set -euo pipefail

DISTRO_VERSION="${1:?usage: $0 DISTRO:VERSION NAME}"
NAME="${2:?usage: $0 DISTRO:VERSION NAME}"

# Keep in lockstep with the "go" line in go.mod so golden VMs match the
# toolchain CI/local dev actually builds with.
GO_VERSION="$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)"
[[ -n "$GO_VERSION" ]] || { echo "provision-golden-orb: could not read go version from go.mod" >&2; exit 1; }

log() { echo "[provision-golden-orb] $*"; }

if orb list 2>/dev/null | awk '{print $1}' | grep -qx "$NAME"; then
	log "machine $NAME already exists, reprovisioning in place"
else
	log "creating $NAME from $DISTRO_VERSION"
	orb create "$DISTRO_VERSION" "$NAME"
fi

# NOTE on style below: orbctl has a real bug where a remote "bash -lc"
# script run under "set -e" that hits an explicit `exit 0` inside a nested
# `if` returns exit 1 through `orb run`, even though the same script exits 0
# in a local shell (reproduced directly against the orb CLI, not specific to
# this script). Every block here is written with a single-level `if` and no
# explicit `exit` to sidestep it rather than relying on `set -e` semantics
# holding remotely.

# testmatrix's Orb.Run always runs suite commands via "bash -lc"; minimal
# distro images (alpine) ship only busybox sh, so ensure bash exists before
# anything else touches this VM. Runs over "sh -c" since bash itself may not
# exist yet.
orb run -m "$NAME" sh -c '
	if ! command -v bash >/dev/null 2>&1; then
		if command -v apk >/dev/null 2>&1; then sudo apk add --no-cache bash
		elif command -v apt-get >/dev/null 2>&1; then sudo apt-get update -qq && sudo apt-get install -y -qq bash
		elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y -q bash
		else echo "no known package manager to install bash" >&2; false
		fi
	fi
'

# gcc for cgo (go test -race needs it).
orb run -m "$NAME" bash -lc '
	if ! command -v gcc >/dev/null 2>&1; then
		if command -v apk >/dev/null 2>&1; then sudo apk add --no-cache build-base
		elif command -v apt-get >/dev/null 2>&1; then sudo apt-get update -qq && sudo apt-get install -y -qq gcc
		elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y -q gcc
		else echo "no known package manager to install gcc" >&2; false
		fi
	fi
'

# node: internal/monitor's test suite spawns real node processes as service
# fixtures, so this is needed for the non-nightly "lifecycle" suite too, not
# just the nightly fixtures suite.
orb run -m "$NAME" bash -lc '
	if ! command -v node >/dev/null 2>&1; then
		if command -v apk >/dev/null 2>&1; then sudo apk add --no-cache nodejs npm
		elif command -v apt-get >/dev/null 2>&1; then sudo apt-get update -qq && sudo apt-get install -y -qq nodejs npm
		elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y -q nodejs npm
		else echo "no known package manager to install node" >&2; false
		fi
	fi
'

ARCH="$(orb run -m "$NAME" uname -m | tr -d '[:space:]' | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
log "installing go $GO_VERSION ($ARCH) on $NAME"

orb run -m "$NAME" bash -lc "
set -euo pipefail
current=''
if command -v go >/dev/null 2>&1 || [[ -x /usr/local/go/bin/go ]]; then
  current=\$(/usr/local/go/bin/go version 2>/dev/null | awk '{print \$3}' | sed 's/^go//')
fi
if [[ \"\$current\" = \"$GO_VERSION\" ]]; then
  echo 'go $GO_VERSION already installed, skipping'
else
  curl -fsSL -o /tmp/go.tar.gz \"https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz\"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf /tmp/go.tar.gz
  rm /tmp/go.tar.gz
fi
/usr/local/go/bin/go version
"

# Warm the module cache on the golden VM itself. Each matrix run clones this
# VM fresh -- an empty module cache means every clone re-downloads the whole
# dependency graph over network on first go test/build, which is slow and,
# observed in practice, occasionally flaky (a transient DNS/network hiccup
# right after clone boot failed a whole suite run even though the code under
# test was fine). Downloading once here means clones inherit a warm cache.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log "warming go module cache on $NAME"
orb run -m "$NAME" -w "$REPO_ROOT" bash -lc 'export PATH=/usr/local/go/bin:$PATH; go mod download'

log "$NAME provisioned"
