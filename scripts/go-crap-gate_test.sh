#!/usr/bin/env bash
# Real, non-mocked end-to-end test for scripts/go-crap-gate.sh's
# OS_INTEGRATION_EXEMPT allowlist and CRAP threshold. A manual verification
# (temp commit -> run gate -> revert) is easy to run once and forget; this is
# that same verification made permanent and re-runnable by anyone (or CI),
# instead of living only in a session transcript.
#
# Proves both directions with the real script, not a copy of its logic:
#   A) touching a real allowlisted OS-integration function must NOT fail the
#      gate at CRAP 20 (the exemption actually exempts).
#   B) touching a function that is deterministically over CRAP 20 and NOT in
#      the allowlist must still fail the gate (the gate is not an accidental
#      no-op).
#
# Direction B used to target a real repo function picked by hand for
# currently exceeding threshold. That's fragile in two ways this test hit in
# practice: (1) it's a moving target -- fixing that function's complexity or
# coverage (the whole point of this repo's genuine-fix issues) silently turns
# the test into a false pass, and (2) build-tag-gated files (e.g. a
# `_linux.go` source) have coverage that differs by platform, so a target
# picked on one OS can pass locally while failing in Linux CI for a real
# reason. Direction B instead commits a brand-new, throwaway package
# containing a function whose complexity and (lack of) coverage are fixed by
# construction -- see gen_scratch_package below -- so its CRAP score is a
# fact about this test, never about the rest of the repo or the platform.
#
# Each direction is a real, temporary commit (git-hook-free: built with
# commit-tree/update-ref, not `git commit`, so it can't trigger lefthook and
# doesn't need a repo-wide git identity), immediately reverted in-place
# afterward -- nothing here is meant to survive past this script's exit.
#
# Slow-ish (go-crap-gate.sh runs a whole-repo `go-crap scan` per direction,
# ~1 min each): that cost buys a real end-to-end proof instead of a unit test
# against a hand-copied fragment of the gate's logic.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

GATE="scripts/go-crap-gate.sh"

# Real allowlisted entry from OS_INTEGRATION_EXEMPT (kept in sync by hand;
# if this signature stops matching, OS_INTEGRATION_EXEMPT's file/function
# pairing likely needs the same update).
EXEMPT_FILE="internal/process/daemon.go"
EXEMPT_SIG="func (d *daemon) wait() {"

# Brand-new scratch package for direction B (must not exist beforehand --
# this test creates and deletes it every run).
SCRATCH_DIR="scripts/gocrapscratch"
SCRATCH_FILE="$SCRATCH_DIR/scratch.go"

if ! git diff --quiet -- "$EXEMPT_FILE" || ! git diff --cached --quiet -- "$EXEMPT_FILE"; then
  echo "go-crap-gate_test: $EXEMPT_FILE has uncommitted changes; aborting rather than risk clobbering them." >&2
  exit 1
fi
if [[ -e "$SCRATCH_DIR" ]]; then
  echo "go-crap-gate_test: $SCRATCH_DIR already exists; aborting rather than risk clobbering it (this test owns that path exclusively)." >&2
  exit 1
fi

BASE_SHA="$(git rev-parse HEAD)"

# Restores a path to its state at BASE_SHA, whether that means putting back
# modified content (path existed before) or deleting it entirely (path is
# this test's own scratch creation and didn't exist before).
revert_path() {
  local path="$1"
  git update-ref HEAD "$BASE_SHA"
  if git cat-file -e "$BASE_SHA:$path" 2>/dev/null; then
    git restore --source="$BASE_SHA" --staged --worktree -- "$path"
  else
    git reset --quiet -- "$path" >/dev/null 2>&1 || true
    rm -f "$path"
    rmdir "$(dirname "$path")" 2>/dev/null || true
  fi
}

cleanup() {
  revert_path "$EXEMPT_FILE"
  revert_path "$SCRATCH_FILE"
}
trap cleanup EXIT

# Stages $file and moves HEAD to a synthetic commit on top of $BASE_SHA --
# via commit-tree + update-ref, not `git commit`, so no pre-commit hook runs
# and no git identity needs to be configured beyond what commit-tree is told
# inline.
synthetic_commit() {
  local file="$1" msg="$2"
  git add "$file"
  local tree commit
  tree="$(git write-tree)"
  commit="$(git -c user.name="go-crap-gate_test" -c user.email="go-crap-gate-test@localhost" \
    commit-tree "$tree" -p "$BASE_SHA" -m "$msg")"
  git update-ref -m "go-crap-gate_test" HEAD "$commit"
}

# Inserts a no-op comment on the line after $sig in $file.
touch_existing_function() {
  local file="$1" sig="$2"
  python3 - "$file" "$sig" <<'PY'
import sys
file, sig = sys.argv[1], sys.argv[2]
lines = open(file, encoding="utf-8").read().splitlines(keepends=True)
idx = next((i for i, l in enumerate(lines) if sig in l), None)
if idx is None:
    print(f"go-crap-gate_test: could not locate signature {sig!r} in {file} "
          "(it moved or was renamed -- update this test's *_SIG constant).",
          file=sys.stderr)
    sys.exit(1)
indent = "\t" if lines[idx].startswith("\t") else ""
lines.insert(idx + 1, f"{indent}// go-crap-gate_test: synthetic touch (reverted automatically)\n")
open(file, "w", encoding="utf-8").writelines(lines)
PY
}

# Writes a brand-new package whose one function has cyclomatic complexity 11
# (McCabe base 1 + 10 independent `if` branches -- go-crap's own counter,
# internal/complexity/complexity.go, adds exactly 1 per *ast.IfStmt) and, at
# the 0% coverage go-crap will measure for it (nothing anywhere tests this
# package), scores CRAP = 11^2*(1-0)^3 + 11 = 132 under go-crap's own formula
# (internal/score/score.go: CRAP(c,cov) = c^2*(1-cov)^3 + c). 132 is so far
# over any threshold this gate has used, past or future, that no realistic
# threshold change threatens this test independent of the rest of the repo.
# No build tags, so it compiles (and is coverage-measured as 0%, not
# "missing") identically on every OS -- unlike a real file that happens to be
# platform-gated.
gen_scratch_package() {
  mkdir -p "$SCRATCH_DIR"
  cat > "$SCRATCH_FILE" <<'EOF'
// Package gocrapscratch is a throwaway target scripts/go-crap-gate_test.sh
// creates and deletes on every run, direction B's over-threshold, deliberately
// uncovered function. It must never appear in a real commit.
package gocrapscratch

func alwaysOverThreshold(n int) int {
	r := n
	if n == 1 {
		r += 1
	}
	if n == 2 {
		r += 2
	}
	if n == 3 {
		r += 3
	}
	if n == 4 {
		r += 4
	}
	if n == 5 {
		r += 5
	}
	if n == 6 {
		r += 6
	}
	if n == 7 {
		r += 7
	}
	if n == 8 {
		r += 8
	}
	if n == 9 {
		r += 9
	}
	if n == 10 {
		r += 10
	}
	return r
}
EOF
}

echo "go-crap-gate_test: direction A -- exempt function must NOT fail the gate at CRAP 20"
touch_existing_function "$EXEMPT_FILE" "$EXEMPT_SIG"
synthetic_commit "$EXEMPT_FILE" "test: touch exempt function (synthetic, auto-reverted)"
set +e
OUT_A="$(GO_CRAP_BASE="$BASE_SHA" GO_CRAP_THRESHOLD=20 bash "$GATE" 2>&1)"
STATUS_A=$?
set -e
echo "$OUT_A"
revert_path "$EXEMPT_FILE"
if [[ "$STATUS_A" -ne 0 ]]; then
  echo "FAIL: touching $EXEMPT_FILE ($EXEMPT_SIG) tripped the gate (exit $STATUS_A) -- OS_INTEGRATION_EXEMPT is not exempting it." >&2
  exit 1
fi
echo "PASS: exempt function did not trip the gate."
echo

echo "go-crap-gate_test: direction B -- a deterministically over-threshold, non-exempt function must still fail the gate at CRAP 20"
gen_scratch_package
synthetic_commit "$SCRATCH_FILE" "test: add synthetic over-threshold function (auto-reverted)"
set +e
OUT_B="$(GO_CRAP_BASE="$BASE_SHA" GO_CRAP_THRESHOLD=20 bash "$GATE" 2>&1)"
STATUS_B=$?
set -e
echo "$OUT_B"
revert_path "$SCRATCH_FILE"
if [[ "$STATUS_B" -eq 0 ]]; then
  echo "FAIL: the synthetic over-threshold function did not trip the gate -- it may have become a no-op." >&2
  exit 1
fi
echo "PASS: synthetic over-threshold function correctly tripped the gate."
echo

echo "go-crap-gate_test: OK -- both directions verified against the real gate script."
