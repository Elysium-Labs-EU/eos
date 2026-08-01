#!/usr/bin/env bash
# Real, non-mocked end-to-end test for scripts/check-plugin-api-diff.sh.
#
# Proves both directions with the real script, not a copy of its logic:
#   A) adding a new exported field to types.LogSink is a compatible change
#      and must PASS.
#   B) renaming an existing exported field of types.LogSink is an
#      incompatible change and must FAIL.
#
# Each direction is a real, temporary commit (git-hook-free: built with
# commit-tree/update-ref, not `git commit`), immediately reverted afterward --
# mirrors scripts/go-crap-gate_test.sh's approach. Direction B only exercises
# apidiff's own type-level analysis (no `go build ./...` runs against the
# renamed field), so a stale reference elsewhere in the tree is harmless and
# never actually lands.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

GATE="scripts/check-plugin-api-diff.sh"
TARGET_FILE="internal/types/types.go"

if ! git diff --quiet -- "$TARGET_FILE" || ! git diff --cached --quiet -- "$TARGET_FILE"; then
  echo "check-plugin-api-diff_test: $TARGET_FILE has uncommitted changes; aborting rather than risk clobbering them." >&2
  exit 1
fi

BASE_SHA="$(git rev-parse HEAD)"

revert() {
  git update-ref HEAD "$BASE_SHA"
  git restore --source="$BASE_SHA" --staged --worktree -- "$TARGET_FILE"
}
trap revert EXIT

synthetic_commit() {
  local msg="$1"
  git add "$TARGET_FILE"
  local tree commit
  tree="$(git write-tree)"
  commit="$(git -c user.name="check-plugin-api-diff_test" -c user.email="check-plugin-api-diff-test@localhost" \
    commit-tree "$tree" -p "$BASE_SHA" -m "$msg")"
  git update-ref -m "check-plugin-api-diff_test" HEAD "$commit"
}

fail=0

# A) add a new exported field to LogSink -- compatible, must pass.
python3 - "$TARGET_FILE" <<'PY'
import sys
f = sys.argv[1]
s = open(f, encoding="utf-8").read()
marker = "\tRestartDelayMs int            `json:\"restart_delay_ms,omitempty\" yaml:\"restart_delay_ms,omitempty\"`\n"
assert marker in s, "check-plugin-api-diff_test: LogSink.RestartDelayMs line moved; update this test's marker"
s = s.replace(marker, marker + "\tAPIDiffTestScratchField string `json:\"api_diff_test_scratch_field,omitempty\"`\n", 1)
open(f, "w", encoding="utf-8").write(s)
PY
synthetic_commit "check-plugin-api-diff_test: add field (expect pass)"
if CHECK_API_DIFF_BASE="$BASE_SHA" bash "$GATE" >/tmp/check-plugin-api-diff-test-a.log 2>&1; then
  echo "PASS: new exported field on LogSink passes the gate"
else
  echo "FAIL: new exported field on LogSink was rejected:"
  cat /tmp/check-plugin-api-diff-test-a.log
  fail=1
fi
revert

# B) rename an existing exported field on LogSink -- incompatible, must fail.
python3 - "$TARGET_FILE" <<'PY'
import sys
f = sys.argv[1]
s = open(f, encoding="utf-8").read()
old = "\tRestartDelayMs int            `json:\"restart_delay_ms,omitempty\" yaml:\"restart_delay_ms,omitempty\"`\n"
new = "\tRestartDelayMsRenamed int    `json:\"restart_delay_ms,omitempty\" yaml:\"restart_delay_ms,omitempty\"`\n"
assert old in s, "check-plugin-api-diff_test: LogSink.RestartDelayMs line moved; update this test's marker"
s = s.replace(old, new, 1)
open(f, "w", encoding="utf-8").write(s)
PY
synthetic_commit "check-plugin-api-diff_test: rename field (expect fail)"
if CHECK_API_DIFF_BASE="$BASE_SHA" bash "$GATE" >/tmp/check-plugin-api-diff-test-b.log 2>&1; then
  echo "FAIL: renamed LogSink field was not rejected:"
  cat /tmp/check-plugin-api-diff-test-b.log
  fail=1
else
  echo "PASS: renamed LogSink field correctly rejected"
fi

if [ "$fail" -ne 0 ]; then
  echo "check-plugin-api-diff_test: FAILED"
  exit 1
fi
echo "check-plugin-api-diff_test: all directions verified OK"
