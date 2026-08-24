#!/usr/bin/env bash
# Fails if the golangci-lint version stops having exactly one source of truth.
#
# The version lives in .golangci-lint-version and is read by taskfiles/lint.yml
# (lint: and fix:), the pre-commit hook (lefthook.yml), and the release
# workflow. Nothing about that arrangement is self-enforcing: a hardcoded
# version in a workflow, or a Taskfile/lefthook command calling whatever
# golangci-lint happens to be on PATH, reintroduces the split quietly and
# only shows up when two of them disagree about a specific line.
#
# eos is the reference this pin arrangement was copied from into the
# eos-sink-* plugin repos (see eos-plugins/scripts/check-golangci-pin.sh) --
# this script is that same guard adapted for eos's single-module layout.
set -euo pipefail

cd "$(dirname "$0")/.."

readonly VERSION_FILE=".golangci-lint-version"
failures=0

fail() {
    echo "check-golangci-pin: $1" >&2
    failures=$((failures + 1))
}

if [ ! -f "$VERSION_FILE" ]; then
    fail "${VERSION_FILE} is missing; it is the single source of the linter version"
    exit 1
fi

version="$(tr -d '[:space:]' <"$VERSION_FILE")"

# A floating minor (v2.11) silently picks up new patch releases, so the same
# tree can start failing on a day nobody touched it. Dependabot does not bump
# this value -- it only rewrites `uses:` refs -- so the pin is deliberate and
# has to be exact to mean anything.
if ! printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
    fail "${VERSION_FILE} holds '${version}', which is not an exact version (want e.g. v2.12.2)"
fi

for workflow in .github/workflows/*.yml; do
    # Only a step reading from the file (or referencing its output) may sit
    # next to the linter action; a literal `version: vX.Y.Z` is a second
    # source of truth that can silently drift from the file.
    if grep -n 'golangci' "$workflow" >/dev/null 2>&1; then
        if grep -nE '^\s*version:\s*v[0-9]' "$workflow" >/dev/null 2>&1; then
            fail "${workflow} hardcodes a golangci-lint version; read ${VERSION_FILE} instead"
            grep -nE '^\s*version:\s*v[0-9]' "$workflow" | sed 's/^/    /' >&2
        fi
    fi
done

# A bare `golangci-lint run|fmt` resolves from PATH, which is the divergence
# this whole arrangement exists to prevent. Both the lint: and fix: tasks
# must go through the pinned `go run .../golangci-lint@{{.GOLANGCI_LINT_VERSION}}`
# form instead.
readonly TASKFILE="taskfiles/lint.yml"

if grep -nE '^\s*-\s*golangci-lint (run|fmt)' "$TASKFILE" >/dev/null 2>&1; then
    fail "${TASKFILE} calls golangci-lint from PATH; use {{.GOLANGCI_LINT_VERSION}}"
    grep -nE '^\s*-\s*golangci-lint (run|fmt)' "$TASKFILE" | sed 's/^/    /' >&2
fi

for target in lint fix; do
    # Extract the task's cmds: lines (2-space-indented body following its
    # "  name:" header, up to the next task at the same indent) and require
    # the pinned invocation to appear somewhere in them.
    recipe="$(awk -v t="^  ${target}:" '
        $0 ~ t { in_target=1; next }
        in_target && /^  [a-zA-Z_-]+:/ { in_target=0 }
        in_target { print }
    ' "$TASKFILE")"
    if [ -z "$recipe" ]; then
        fail "${TASKFILE} has no ${target}: task"
    elif ! printf '%s\n' "$recipe" | grep -q 'golangci-lint/v2/cmd/golangci-lint@{{.GOLANGCI_LINT_VERSION}}'; then
        fail "${TASKFILE}'s ${target}: task does not use the pinned golangci-lint invocation"
    fi
done

# lefthook.yml runs golangci-lint directly (not through make), so it must
# resolve the same file itself rather than hardcoding a version or falling
# back to PATH.
if grep -q 'golangci-lint' lefthook.yml 2>/dev/null; then
    if grep -nE 'golangci-lint@v[0-9]' lefthook.yml >/dev/null 2>&1; then
        fail "lefthook.yml hardcodes a golangci-lint version; read ${VERSION_FILE} instead"
        grep -nE 'golangci-lint@v[0-9]' lefthook.yml | sed 's/^/    /' >&2
    fi
    if ! grep -q "cat \"\$(git rev-parse --show-toplevel)/${VERSION_FILE}\"" lefthook.yml; then
        fail "lefthook.yml does not read ${VERSION_FILE}"
    fi
fi

if [ "$failures" -ne 0 ]; then
    echo "check-golangci-pin: ${failures} problem(s) found" >&2
    exit 1
fi

echo "check-golangci-pin: OK, golangci-lint pinned at ${version} in ${VERSION_FILE}"
