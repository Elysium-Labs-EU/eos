#!/bin/bash
# Focused, network-free test for install.sh's URL building and JSON parsing.
# Guards against regressing to a Codeberg host and against GitHub's spaced
# JSON breaking the tag parsers (Gitea returns compact JSON, GitHub
# pretty-prints with a space after each colon).
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../install.sh
source "$DIR/install.sh"

failures=0

assert_eq() {
    local actual="$1"
    local expected="$2"
    local desc="$3"

    if [ "$actual" = "$expected" ]; then
        echo "ok   - $desc"
    else
        echo "FAIL - $desc"
        echo "       expected: $expected"
        echo "       actual:   $actual"
        failures=$((failures + 1))
    fi
}

# extract_tag_name must parse GitHub's spaced JSON.
result=$(printf '%s' '{
  "tag_name": "v1.2.3",
  "prerelease": false
}' | extract_tag_name)
assert_eq "$result" "v1.2.3" "extract_tag_name parses GitHub's spaced JSON"

# pick_latest_tag must still parse Gitea's compact JSON (regression guard)
# and must prefer the highest stable tag over a newer prerelease.
result=$(printf '%s' '[{"tag_name":"v0.0.9","prerelease":true},{"tag_name":"v0.0.8","prerelease":false}]' | pick_latest_tag)
assert_eq "$result" "v0.0.8" "pick_latest_tag prefers stable over a newer prerelease, parses compact JSON"

# fetch_latest_version must resolve against GitHub's API host, not Codeberg's,
# and must fall back to the full /releases list (via pick_latest_tag) when
# /releases/latest 404s (e.g. every release so far is a prerelease).
curl() {
    local url="$*"
    case "$url" in
        *api.github.com/repos/Elysium-Labs-EU/eos/releases/latest*) return 1 ;;
        *api.github.com/repos/Elysium-Labs-EU/eos/releases?per_page=100*)
            printf '%s' '[{"tag_name":"v0.0.12-rc.5","prerelease":true},{"tag_name":"v0.0.12-rc.4","prerelease":true}]'
            ;;
        *) return 1 ;;
    esac
}
result=$(fetch_latest_version "curl")
assert_eq "$result" "v0.0.12-rc.5" "fetch_latest_version targets GitHub API and falls back to the releases list"
unset -f curl

# Download and checksum URLs must point at github.com, not Codeberg.
result=$(build_download_url "$REPO" "v1.2.3" "eos-linux-amd64")
assert_eq "$result" "https://github.com/Elysium-Labs-EU/eos/releases/download/v1.2.3/eos-linux-amd64" "build_download_url targets github.com"

result=$(build_download_url "$REPO" "v1.2.3" "sha256sums.txt")
assert_eq "$result" "https://github.com/Elysium-Labs-EU/eos/releases/download/v1.2.3/sha256sums.txt" "build_download_url builds checksums URL"

if [ "$failures" -ne 0 ]; then
    echo ""
    echo "$failures assertion(s) failed"
    exit 1
fi

echo ""
echo "All install.sh assertions passed"
