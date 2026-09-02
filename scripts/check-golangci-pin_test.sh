#!/usr/bin/env bash
# Real, non-mocked end-to-end test for scripts/check-golangci-pin.sh.
#
# Unlike check-diff-size.sh, this gate reads working-tree files directly
# (not a git diff), so the test mutates real tracked files in place and
# restores them afterward -- there is no synthetic-commit indirection needed.
#
# Proves each drift the gate exists to catch:
#   A) clean tree (today's state) must PASS.
#   B) a non-exact .golangci-lint-version (e.g. "latest") must FAIL.
#   C) a workflow hardcoding `version: vX.Y.Z` next to golangci must FAIL.
#   D) taskfiles/lint.yml's fix: task calling bare `golangci-lint fmt` must FAIL.
#   E) lefthook.yml hardcoding a version instead of reading the file must FAIL.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

GATE="scripts/check-golangci-pin.sh"
VERSION_FILE=".golangci-lint-version"
RELEASE_WORKFLOW=".github/workflows/release.yml"

backup_dir="$(mktemp -d)"
TASKFILE="taskfiles/lint.yml"

cp "$VERSION_FILE" "$backup_dir/version"
cp "$RELEASE_WORKFLOW" "$backup_dir/release.yml"
cp "$TASKFILE" "$backup_dir/lint.yml"
cp lefthook.yml "$backup_dir/lefthook.yml"

restore() {
    cp "$backup_dir/version" "$VERSION_FILE"
    cp "$backup_dir/release.yml" "$RELEASE_WORKFLOW"
    cp "$backup_dir/lint.yml" "$TASKFILE"
    cp "$backup_dir/lefthook.yml" lefthook.yml
    rm -rf "$backup_dir"
}
trap restore EXIT

fail=0

# A) clean tree -- must pass.
if bash "$GATE" >/tmp/check-golangci-pin-test-a.log 2>&1; then
    echo "PASS: clean tree passes the gate"
else
    echo "FAIL: clean tree was rejected:"
    cat /tmp/check-golangci-pin-test-a.log
    fail=1
fi

# B) floating/non-exact version -- must fail.
printf 'latest\n' > "$VERSION_FILE"
if bash "$GATE" >/tmp/check-golangci-pin-test-b.log 2>&1; then
    echo "FAIL: non-exact version 'latest' was not rejected:"
    cat /tmp/check-golangci-pin-test-b.log
    fail=1
else
    echo "PASS: non-exact version correctly rejected"
fi
cp "$backup_dir/version" "$VERSION_FILE"

# C) workflow hardcoding a version next to golangci -- must fail.
printf '\n  golangci-hardcode-check:\n    steps:\n      - uses: golangci/golangci-lint-action@x\n        with:\n          version: v2.0.0\n' >> "$RELEASE_WORKFLOW"
if bash "$GATE" >/tmp/check-golangci-pin-test-c.log 2>&1; then
    echo "FAIL: hardcoded workflow version was not rejected:"
    cat /tmp/check-golangci-pin-test-c.log
    fail=1
else
    echo "PASS: hardcoded workflow version correctly rejected"
fi
cp "$backup_dir/release.yml" "$RELEASE_WORKFLOW"

# D) taskfiles/lint.yml fix: task reverted to a bare, unpinned call -- must fail.
sed -i.bak 's|go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{\.GOLANGCI_LINT_VERSION}} fmt|golangci-lint fmt|' "$TASKFILE"
rm -f "${TASKFILE}.bak"
if bash "$GATE" >/tmp/check-golangci-pin-test-d.log 2>&1; then
    echo "FAIL: bare 'golangci-lint fmt' in ${TASKFILE} was not rejected:"
    cat /tmp/check-golangci-pin-test-d.log
    fail=1
else
    echo "PASS: bare ${TASKFILE} invocation correctly rejected"
fi
cp "$backup_dir/lint.yml" "$TASKFILE"

# E) lefthook.yml hardcoding a version -- must fail.
sed -i.bak 's|golangci-lint@\$(cat "\$(git rev-parse --show-toplevel)/\.golangci-lint-version")|golangci-lint@v2.0.0|g' lefthook.yml
rm -f lefthook.yml.bak
if bash "$GATE" >/tmp/check-golangci-pin-test-e.log 2>&1; then
    echo "FAIL: hardcoded lefthook.yml version was not rejected:"
    cat /tmp/check-golangci-pin-test-e.log
    fail=1
else
    echo "PASS: hardcoded lefthook.yml version correctly rejected"
fi
cp "$backup_dir/lefthook.yml" lefthook.yml

if [[ "$fail" -ne 0 ]]; then
    echo "check-golangci-pin_test: FAILED"
    exit 1
fi
echo "check-golangci-pin_test: all directions verified OK"
