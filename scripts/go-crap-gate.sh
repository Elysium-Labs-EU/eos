#!/usr/bin/env bash
# Change-scoped go-crap gate ("Change-Risk Analysis").
#
# go-crap has no native diff/baseline mode, so this wrapper runs a full CRAP
# scan (coverage needs the whole module) and then fails ONLY on functions whose
# own lines this change modified relative to the base ref. Pre-existing debt in
# untouched functions -- even in a file you edited elsewhere -- does not block.
#
#   GO_CRAP_BASE       base ref (default: origin/main); CI sets it to PR target
#   GO_CRAP_THRESHOLD  CRAP threshold (default: 30, go-crap's own default)
#
# Pure bash + python3 (no gawk), so it runs the same on macOS and Linux CI.
set -euo pipefail

THRESHOLD="${GO_CRAP_THRESHOLD:-30}"
BASE="${GO_CRAP_BASE:-origin/main}"

command -v go-crap >/dev/null 2>&1 || {
  echo "go-crap not found. Run: go install github.com/padiazg/go-crap@latest" >&2
  exit 1
}

# CI checkouts are often shallow; make the base ref resolvable.
if ! git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  git fetch --quiet origin "${BASE#origin/}" 2>/dev/null || true
fi
if git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  DIFF_BASE="$(git merge-base "$BASE" HEAD 2>/dev/null || echo "$BASE")"
else
  echo "go-crap-gate: base ref '$BASE' unresolvable; nothing to compare against, passing." >&2
  exit 0
fi

CHANGED_GO="$(git diff --name-only "$DIFF_BASE" HEAD -- '*.go' | grep -v '_test\.go$' || true)"
if [ -z "$CHANGED_GO" ]; then
  echo "go-crap-gate: no non-test Go files changed vs $BASE; nothing to gate."
  exit 0
fi

TMP_JSON="$(mktemp -t gocrap.XXXXXX)"
trap 'rm -f "$TMP_JSON"' EXIT
go-crap scan . --exclude '.*_test\.go' --format json -o "$TMP_JSON"

python3 - "$DIFF_BASE" "$THRESHOLD" "$TMP_JSON" <<'PY'
import json, re, subprocess, sys

diff_base = sys.argv[1]
threshold = float(sys.argv[2])
entries = json.load(open(sys.argv[3]))["entries"]

# Non-test Go files changed vs the base.
changed_files = [
    f for f in subprocess.run(
        ["git", "diff", "--name-only", diff_base, "HEAD", "--", "*.go"],
        capture_output=True, text=True, check=True,
    ).stdout.split()
    if not f.endswith("_test.go")
]

# New-side line numbers touched in each changed file (from -U0 hunk headers).
hunk_re = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
changed_lines = {}
for f in changed_files:
    out = subprocess.run(
        ["git", "diff", "-U0", diff_base, "HEAD", "--", f],
        capture_output=True, text=True, check=True,
    ).stdout
    touched = set()
    for line in out.splitlines():
        m = hunk_re.match(line)
        if not m:
            continue
        start = int(m.group(1))
        count = int(m.group(2)) if m.group(2) else 1
        for i in range(count):
            touched.add(start + i)
    if touched:
        changed_lines[f] = touched

# Per file, a function "owns" line L if it has the greatest start line <= L.
by_file = {}
for e in entries:
    by_file.setdefault(e["file"], []).append(e)
for lst in by_file.values():
    lst.sort(key=lambda e: e["line"])

def owner(f, line):
    found = None
    for e in by_file.get(f, []):
        if e["line"] <= line:
            found = e
        else:
            break
    return found

touched_funcs = {}
for f, lines in changed_lines.items():
    for L in lines:
        e = owner(f, L)
        if e:
            touched_funcs[(e["file"], e["function"], e["line"])] = e

bad = [e for e in touched_funcs.values() if e["crap"] > threshold]
if bad:
    print(f"go-crap gate FAILED: {len(bad)} changed function(s) exceed CRAP {threshold:g}:")
    for e in sorted(bad, key=lambda x: -x["crap"]):
        print(f'  CRAP {e["crap"]:8.2f}  cov {e["coverage"]:5.1f}%  '
              f'{e["function"]}  ({e["file"]}:{e["line"]})')
    print("\nReduce complexity or add test coverage for these functions, or split them.")
    sys.exit(1)
print(f"go-crap gate OK: {len(touched_funcs)} changed function(s), none exceed CRAP {threshold:g}.")
PY
