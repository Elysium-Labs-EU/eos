#!/usr/bin/env bash
# Real, non-mocked end-to-end test for scripts/check-diff-size.sh.
#
# Proves three directions with the real script, not a copy of its logic:
#   A) a normal small text file added on top of base must PASS.
#   B) a file over the size cap must FAIL.
#   C) a committed Git-LFS pointer blob must FAIL, even under the cap.
#
# Each direction is a real, temporary commit (git-hook-free: built with
# commit-tree/update-ref, not `git commit`, so it can't trigger lefthook and
# doesn't need a repo-wide git identity), immediately reverted in-place
# afterward -- mirrors scripts/go-crap-gate_test.sh's approach.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

GATE="scripts/check-diff-size.sh"
SCRATCH_DIR="scripts/diffsizescratch"
SCRATCH_FILE="$SCRATCH_DIR/scratch.bin"

if [ -e "$SCRATCH_DIR" ]; then
  echo "check-diff-size_test: $SCRATCH_DIR already exists; aborting rather than risk clobbering it (this test owns that path exclusively)." >&2
  exit 1
fi

BASE_SHA="$(git rev-parse HEAD)"

revert() {
  git update-ref HEAD "$BASE_SHA"
  git reset --quiet -- "$SCRATCH_DIR" >/dev/null 2>&1 || true
  rm -rf "$SCRATCH_DIR"
}
trap revert EXIT

# Stages $SCRATCH_FILE and moves HEAD to a synthetic commit on top of BASE_SHA.
synthetic_commit() {
  local msg="$1"
  git add "$SCRATCH_FILE"
  local tree commit
  tree="$(git write-tree)"
  commit="$(git -c user.name="check-diff-size_test" -c user.email="check-diff-size-test@localhost" \
    commit-tree "$tree" -p "$BASE_SHA" -m "$msg")"
  git update-ref -m "check-diff-size_test" HEAD "$commit"
}

fail=0

# A) small file, default cap -- must pass.
mkdir -p "$SCRATCH_DIR"
printf 'hello world\n' > "$SCRATCH_FILE"
synthetic_commit "check-diff-size_test: small file (expect pass)"
if CHECK_DIFF_SIZE_BASE="$BASE_SHA" bash "$GATE" >/tmp/check-diff-size-test-a.log 2>&1; then
  echo "PASS: small file under default cap passes the gate"
else
  echo "FAIL: small file under default cap was rejected:"
  cat /tmp/check-diff-size-test-a.log
  fail=1
fi
git update-ref HEAD "$BASE_SHA"
git reset --quiet -- "$SCRATCH_DIR" >/dev/null 2>&1 || true
rm -rf "$SCRATCH_DIR"

# B) file over a (lowered, for test speed) cap -- must fail.
mkdir -p "$SCRATCH_DIR"
head -c 200 /dev/zero > "$SCRATCH_FILE"
synthetic_commit "check-diff-size_test: oversized file (expect fail)"
if CHECK_DIFF_SIZE_BASE="$BASE_SHA" CHECK_DIFF_SIZE_MAX_BYTES=100 bash "$GATE" >/tmp/check-diff-size-test-b.log 2>&1; then
  echo "FAIL: 200-byte file did not trip a 100-byte cap:"
  cat /tmp/check-diff-size-test-b.log
  fail=1
else
  echo "PASS: oversized file correctly rejected"
fi
git update-ref HEAD "$BASE_SHA"
git reset --quiet -- "$SCRATCH_DIR" >/dev/null 2>&1 || true
rm -rf "$SCRATCH_DIR"

# C) Git-LFS pointer blob, well under the size cap -- must still fail.
mkdir -p "$SCRATCH_DIR"
cat > "$SCRATCH_FILE" <<'EOF'
version https://git-lfs.github.com/spec/v1
oid sha256:0000000000000000000000000000000000000000000000000000000000000000
size 123
EOF
synthetic_commit "check-diff-size_test: LFS pointer (expect fail)"
if CHECK_DIFF_SIZE_BASE="$BASE_SHA" bash "$GATE" >/tmp/check-diff-size-test-c.log 2>&1; then
  echo "FAIL: LFS pointer blob was not rejected:"
  cat /tmp/check-diff-size-test-c.log
  fail=1
else
  echo "PASS: LFS pointer blob correctly rejected"
fi

if [ "$fail" -ne 0 ]; then
  echo "check-diff-size_test: FAILED"
  exit 1
fi
echo "check-diff-size_test: all directions verified OK"
