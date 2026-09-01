#!/usr/bin/env bash
# Real, non-mocked end-to-end test for scripts/check-arrow-notation.sh.
#
# check-arrow-notation.sh scans the whole working tree (not a diff), so this
# test just drops scratch .md files under a scratch dir at repo root and runs
# the real gate against them -- no synthetic commits needed, unlike
# check-diff-size_test.sh / go-crap-gate_test.sh.
#
# Proves, with the real script, not a copy of its logic:
#   A) plain prose with no arrows -- must PASS.
#   B) ASCII "->" in prose -- must FAIL.
#   C) ASCII "<-" in prose -- must FAIL.
#   D) unicode "→" in prose -- must FAIL.
#   E) HTML arrow entity ("&rarr;") in prose -- must FAIL.
#   F) "->" inside a fenced code block -- must PASS (fences are exempt).
#   G) "-->" (HTML comment close) -- must PASS (not treated as an arrow).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

GATE="scripts/check-arrow-notation.sh"
SCRATCH_DIR="scripts/arrownotationscratch"
SCRATCH_FILE="$SCRATCH_DIR/scratch.md"

if [ -e "$SCRATCH_DIR" ]; then
  echo "check-arrow-notation_test: $SCRATCH_DIR already exists; aborting rather than risk clobbering it (this test owns that path exclusively)." >&2
  exit 1
fi

cleanup() {
  rm -rf "$SCRATCH_DIR"
}
trap cleanup EXIT

mkdir -p "$SCRATCH_DIR"
fail=0

run_case() {
  local label="$1" expect="$2"
  if bash "$GATE" >/tmp/check-arrow-notation-test.log 2>&1; then
    result="pass"
  else
    result="fail"
  fi
  if [ "$result" = "$expect" ]; then
    echo "PASS: $label ($result as expected)"
  else
    echo "FAIL: $label -- expected $expect, got $result:"
    cat /tmp/check-arrow-notation-test.log
    fail=1
  fi
}

# A) plain prose -- must pass.
printf 'Run the task, then check the output.\n' > "$SCRATCH_FILE"
run_case "plain prose" pass

# B) ASCII "->" in prose -- must fail.
printf 'Do this -> then that.\n' > "$SCRATCH_FILE"
run_case 'ASCII "->" in prose' fail

# C) ASCII "<-" in prose -- must fail.
printf 'The value flows back <- from the caller.\n' > "$SCRATCH_FILE"
run_case 'ASCII "<-" in prose' fail

# D) unicode arrow in prose -- must fail.
printf 'Data flows source \xe2\x86\x92 sink.\n' > "$SCRATCH_FILE"
run_case "unicode arrow in prose" fail

# E) HTML arrow entity in prose -- must fail.
printf 'Data flows source &rarr; sink.\n' > "$SCRATCH_FILE"
run_case "HTML arrow entity in prose" fail

# F) "->" inside a fenced code block -- must pass.
printf '```\nMATCH (a)-[:REL]->(b) RETURN a\n```\n' > "$SCRATCH_FILE"
run_case '"->" inside fenced code block' pass

# G) "-->" HTML comment close -- must pass.
printf '<!-- gitnexus:start -->\n' > "$SCRATCH_FILE"
run_case '"-->" HTML comment close' pass

if [ "$fail" -ne 0 ]; then
  echo "check-arrow-notation_test: FAILED"
  exit 1
fi
echo "check-arrow-notation_test: all directions verified OK"
