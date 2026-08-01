#!/usr/bin/env bash
# Breaking-change gate for the cross-process wire contract in internal/types:
# types.LogSink (the sink-plugin config surface: eos-plugins binaries are
# configured from this struct's fields) and the daemon RPC request/response
# types (DaemonRequest/DaemonResponse and friends, the CLI<->daemon socket
# protocol). Both are consumed outside this module's own Go build -- by
# external plugin binaries and by the CLI process across a socket -- so an
# "incompatible" field rename/removal here breaks something apidiff-invisible
# to a normal `go build`.
#
# Not a substitute for the planned capability-handshake/compat-manifest work
# tracked separately for the plugin API; this is the cheap static gate that
# stops an accidental break from shipping in the meantime.
#
#   CHECK_API_DIFF_BASE  base ref (default: origin/main); CI sets it to PR target
#   CHECK_API_DIFF_PKG   package to diff (default: ./internal/types)
#
# Base-ref resolution mirrors scripts/go-crap-gate.sh and scripts/check-diff-size.sh.
set -euo pipefail

BASE="${CHECK_API_DIFF_BASE:-origin/main}"
PKG="${CHECK_API_DIFF_PKG:-./internal/types}"
PKG_DIR="${PKG#./}"

command -v apidiff >/dev/null 2>&1 || {
  echo "apidiff not found. Run: go install golang.org/x/exp/cmd/apidiff@latest" >&2
  exit 1
}

if ! git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  git fetch --quiet origin "${BASE#origin/}" 2>/dev/null || true
fi
if git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  DIFF_BASE="$(git merge-base "$BASE" HEAD 2>/dev/null || echo "$BASE")"
else
  echo "check-plugin-api-diff: base ref '$BASE' unresolvable; nothing to compare against, passing." >&2
  exit 0
fi

CHANGED="$(git diff --name-only "$DIFF_BASE" HEAD -- "$PKG_DIR" || true)"
if [ -z "$CHANGED" ]; then
  echo "check-plugin-api-diff: $PKG_DIR unchanged vs $BASE; nothing to gate."
  exit 0
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# Materialize the base ref's tree into a plain directory (no .git, no worktree
# registration) so apidiff can type-check the old package against its own
# go.mod/go.sum without touching this checkout or any sibling worktree.
OLD_DIR="$TMP_ROOT/old"
mkdir -p "$OLD_DIR"
git archive "$DIFF_BASE" | tar -x -C "$OLD_DIR"

OLD_API="$TMP_ROOT/old.api"
# -allow-internal: apidiff otherwise silently skips packages under internal/
# on the (usually correct) assumption they aren't API -- not true here, see
# the header comment above.
(cd "$OLD_DIR" && apidiff -allow-internal -w "$OLD_API" "$PKG")

OUT="$TMP_ROOT/diff.out"
# apidiff always exits 0 regardless of what it finds -- it's a report, not a
# gate -- so the pass/fail decision here comes from grepping its output.
apidiff -allow-internal "$OLD_API" "$PKG" >"$OUT" 2>&1 || true

if grep -q '^Incompatible changes:' "$OUT"; then
  echo "check-plugin-api-diff FAILED: incompatible change(s) to $PKG_DIR vs $BASE:"
  cat "$OUT"
  exit 1
fi

echo "check-plugin-api-diff OK: no incompatible API changes in $PKG_DIR vs $BASE."
cat "$OUT"
