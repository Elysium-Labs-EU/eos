#!/usr/bin/env bash
# Change-scoped file-size / Git-LFS guard.
#
# Blocks two accidents from landing in history via a PR diff:
#   1. An oversized file (build output, binary asset, dump) committed by mistake.
#   2. A Git-LFS pointer file -- this repo does not use LFS, so a pointer blob
#      means someone ran `git lfs track` locally without the repo actually
#      being LFS-enabled; clones elsewhere would see the pointer text, not
#      the real content.
#
#   CHECK_DIFF_SIZE_BASE       base ref (default: origin/main); CI sets it to PR target
#   CHECK_DIFF_SIZE_MAX_BYTES  per-file size cap in bytes (default: 1048576 / 1MiB)
#
# Mirrors scripts/go-crap-gate.sh's base-ref resolution so both gates behave
# identically in shallow CI checkouts and local runs.
set -euo pipefail

MAX_BYTES="${CHECK_DIFF_SIZE_MAX_BYTES:-1048576}"
BASE="${CHECK_DIFF_SIZE_BASE:-origin/main}"
LFS_HEADER='version https://git-lfs.github.com/spec/v1'

if ! git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  git fetch --quiet origin "${BASE#origin/}" 2>/dev/null || true
fi
if git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  DIFF_BASE="$(git merge-base "$BASE" HEAD 2>/dev/null || echo "$BASE")"
else
  echo "check-diff-size: base ref '$BASE' unresolvable; nothing to compare against, passing." >&2
  exit 0
fi

CHANGED="$(git diff --name-only --diff-filter=ACMR "$DIFF_BASE" HEAD || true)"
if [ -z "$CHANGED" ]; then
  echo "check-diff-size: no added/modified files vs $BASE; nothing to gate."
  exit 0
fi

violations=0
while IFS= read -r f; do
  [ -z "$f" ] && continue

  size="$(git cat-file -s "HEAD:$f" 2>/dev/null || echo 0)"
  if [ "$size" -gt "$MAX_BYTES" ]; then
    echo "  TOO LARGE  $f  (${size} bytes > ${MAX_BYTES} byte cap)"
    violations=$((violations + 1))
  fi

  header="$(git cat-file -p "HEAD:$f" 2>/dev/null | head -c "${#LFS_HEADER}" || true)"
  if [ "$header" = "$LFS_HEADER" ]; then
    echo "  LFS POINTER  $f  (this repo does not use Git LFS -- commit the real file)"
    violations=$((violations + 1))
  fi
done <<< "$CHANGED"

if [ "$violations" -gt 0 ]; then
  echo "check-diff-size FAILED: $violations violation(s) vs $BASE."
  exit 1
fi
echo "check-diff-size OK: $(echo "$CHANGED" | wc -l | tr -d ' ') file(s) checked, none over ${MAX_BYTES} bytes or an LFS pointer."
