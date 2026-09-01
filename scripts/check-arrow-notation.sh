#!/usr/bin/env bash
# Blocks arrow notation in prose (*.md files): bare "->"/"<-", unicode
# arrows ("→"/"←"), and HTML arrow entities ("&rarr;", "&larr;", etc).
#
# Arrows read as pseudo-code, not prose, and this repo's style favors plain
# sentences (see STYLE.md) -- rely on words for actions since terminals
# don't always render arrow characters properly. Fenced code blocks are
# exempt since real code/query syntax (e.g. GitNexus Cypher's -[:REL]->)
# legitimately needs "->". "<!-- -->" HTML comment delimiters are exempt
# too -- they aren't arrows. CHANGELOG.md is exempt since it's
# git-cliff-generated from verbatim historical commit subjects.
set -euo pipefail

failed=0

flag() {
  local f="$1" line_no="$2" why="$3"
  echo "check-arrow-notation: $f:$line_no: $why" >&2
  failed=1
}

while IFS= read -r -d '' f; do
  in_fence=0
  line_no=0
  while IFS= read -r line; do
    line_no=$((line_no + 1))
    if [[ "$line" =~ ^[[:space:]]*\`\`\` ]]; then
      in_fence=$((1 - in_fence))
      continue
    fi
    [[ "$in_fence" -eq 1 ]] && continue

    # "-->" (HTML comment close) is not the arrow notation this checks for.
    if [[ "$line" == *"->"* && "$line" != *"-->"* ]]; then
      flag "$f" "$line_no" 'ASCII "->" in prose, use a plain word instead'
    fi
    if [[ "$line" == *"<-"* ]]; then
      flag "$f" "$line_no" 'ASCII "<-" in prose, use a plain word instead'
    fi
    if [[ "$line" == *"→"* || "$line" == *"←"* ]]; then
      flag "$f" "$line_no" 'unicode arrow in prose, use a plain word instead'
    fi
    if [[ "$line" =~ \&[A-Za-z]*[Aa]rr\; ]]; then
      flag "$f" "$line_no" "HTML arrow entity (${BASH_REMATCH[0]}) in prose, use a plain word instead"
    fi
  done < "$f"
done < <(find . -name '*.md' \
  -not -path './node_modules/*' \
  -not -path './.git/*' \
  -not -path './.claude/worktrees/*' \
  -not -path './CHANGELOG.md' \
  -print0)

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
echo "check-arrow-notation: OK, no arrow notation found in markdown prose."
