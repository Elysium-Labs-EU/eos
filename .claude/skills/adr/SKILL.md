---
name: adr
description: "Use when the user asks to record an architecture decision, write an ADR, or invokes /adr. Also use before making a significant, hard-to-reverse design or infra decision (framework choice, API break, dropped feature, changed invariant) that isn't already covered by an existing ADR. Examples: \"write an ADR for this\", \"record this decision\", \"why did we decide X\" (check docs/adr first), \"/adr <title>\"."
---

# ADR — Architecture Decision Records

Convention for recording a significant, hard-to-reverse decision as a
durable file under `docs/adr/`, so it survives outside commit messages,
closed issues, and chat history. Follow the format exactly — filename
scheme, section order, status vocabulary — every time.

## Before writing one

1. Decide it's worth an ADR: a significant, hard-to-reverse decision with
   real tradeoffs — not a routine bugfix, not a one-line config tweak, not
   anything already fully explained by the code itself.
2. Check `docs/adr/` first. Use the repo's `adr-find` task if it has one
   (whatever runs it — `make adr-find Q="<concept>"`, `task adr-find --
   <concept>`, an npm/pnpm script, a shell alias), or fall back to
   `grep -ril -- "<concept>" docs/adr/*.md` directly if it has none. If an
   existing ADR already covers this ground, your new decision either fits
   inside it (no new file needed) or reverses/narrows it (write a new ADR
   that supersedes the old one — see below).
3. If `docs/adr/` doesn't exist yet in this repo, create it. The first ADR
   in a repo is always "Record architecture decisions" using this same
   convention (see Template).

## Id and filename

`docs/adr/<id>-<slug>.md`

- `<id>` is a minute-precision datetimestamp: `DDMMYYHHMM` (day, month,
  two-digit year, hour, minute, 24h) taken from the moment you create the
  file — e.g. `1408260745`. Get it with `date +%d%m%y%H%M`. This is always
  the write time, even for a retroactive ADR (see `Date:` below for the
  decision's own date).
- Never coordinate a number with anyone. The timestamp self-allocates; two
  authors writing ADRs in the same repo at the same time simply get
  different filenames. A same-minute collision is a plain git add/add
  conflict, same as any other simultaneous add of the same filename —
  resolve it by bumping one file's id by one minute (re-run `date`, or add
  a minute by hand) and renaming just that file. Don't renumber, rename, or
  otherwise touch the other side.
- `<slug>` is a short kebab-case phrase from the title.
- Older repos may still carry legacy ids in other shapes (sequential
  `0001-`, or date-only `YYYY-MM-DD-`) from before this convention was
  adopted there. Never renumber or rename those — an accepted ADR is
  immutable (see below). New ADRs always use the datetimestamp form; the id
  shapes coexist in the same directory indefinitely. Don't rely on
  lexicographic filename sort to reconstruct chronological order across
  mixed id shapes — check each file's own `Date:` line instead.

## File format

```markdown
# <id>. <Title, short imperative phrase>

Date: YYYY-MM-DD
Status: Accepted

## Context

- Bullet points. The forces at play, what's already true, what triggered
  this decision now.

## Decision

- Bullet points. What was decided, stated plainly enough to act on.
- If this supersedes an earlier ADR, say so explicitly and name which of
  its clauses still stand vs. which are overridden. Never edit the old file.

## Rejected

- Alternatives seriously considered, and the specific reason each lost.
  Omit this section entirely if there was really only one option.

## Consequences

- What gets easier, harder, or newly possible. Include concrete follow-on
  effects (a test that now needs to exist, a doc line that's now stale,
  a check that should be added to prevent regression).
```

Rules, no exceptions:

- **`Date:` is the decision's own date, not the write date.** For a
  same-day ADR the two are the same; for a retroactive ADR, `Date:` is when
  the decision actually happened, `<id>` is still today's timestamp (see
  above). Never backdate the id itself — a fabricated id shape breaks the
  "id sorts by creation order" property other tooling relies on.
- **`Status:` has exactly two valid values: `Proposed` and `Accepted`.**
  Nothing else. Write `Proposed` for a decision put up for discussion, not
  yet committed to; `Accepted` once it's decided. A `Proposed` file may
  still be edited in place (revise it as discussion moves the decision) —
  it becomes immutable only once flipped to `Accepted`.
  There is no `Superseded` or `Deprecated` status: because an accepted file
  is never edited, its `Status:` line can never change after acceptance.
  A decision that reverses or narrows an old one is a **new** ADR, `Status:
  Accepted`, whose Decision section says in prose which old id it
  supersedes and which of that id's clauses still stand. The old file's
  `Status: Accepted` line stays exactly as it was — a reader (or
  `adr-find` task) has to read the newer file to learn it was superseded.
- **`Status:` is always an inline line directly under `Date:`.** Never a
  `## Status` heading. The `adr-find` task greps `^Status:` — a heading
  form silently breaks it.
- **Bullets, not prose paragraphs.** Every section.
- **No issue or ticket numbers in the body.** Issues get renumbered, closed,
  or migrated between trackers; the ADR is the permanent record and has to
  stand on its own without one.
- **Immutable once `Status: Accepted`.** A reversed or narrowed decision is
  a brand new ADR that supersedes the old one by number/id, stated in its
  Decision section. The old file is never edited or deleted.
- **No shared index file.** Don't create or append to `docs/adr/index.md`.
  The directory listing is the index; the `adr-find` task (or a plain
  `grep`) is the query interface. A shared index is a merge-conflict
  generator: two branches adding an ADR concurrently both append a row at
  the same anchor point, and that conflict fires independent of numbering.
- **Length:** as short as the decision allows, but complete — don't pad
  Context with backstory the Decision doesn't need, but a genuinely tangled
  decision earns a genuinely longer file. There's no fixed line target.

## Repo plumbing to check/add alongside the first ADR

If the target repo doesn't already have an `adr-find` task, add one when
writing the repo's first ADR (not on every subsequent one) — in whatever
task runner the repo already uses for everything else (Make, Taskfile/
go-task, npm/pnpm scripts, a plain shell script under `scripts/`). Don't
introduce a new task runner just for this. It should:

- Take a free-text query (`Q`, an argument, whatever fits the runner's
  convention).
- `grep -ril` that query against `docs/adr/*.md`.
- For each match, print its path alongside its `Status:` line (`grep -m1
  '^Status:' <file>`), which is always `Proposed` or `Accepted` per the
  two-value Status rule above — a superseded decision won't show as such
  here, that's by design, so check the newer ADR's own text too.
- Optionally, as a secondary non-blocking enrichment step, also grep code
  comments for `ADR-\d{4,}` via ast-grep, degrading to a notice (never a
  failure) if `ast-grep` isn't installed.

Two equivalent models — pick whichever matches the repo's existing task
runner:

```makefile
# Makefile — recipe lines below the target must start with a literal tab,
# not spaces; retype the indent by hand if pasting from a rendered doc, or
# `make` fails with "missing separator".
adr-find: ## Find ADRs and related code for a concept: make adr-find Q="concept"
	@test -n "$(Q)" || { echo "Usage: make adr-find Q=\"concept\""; exit 1; }
	@echo "--- docs/adr matching \"$(Q)\" ---"
	@hits="$$(grep -ril -- "$(Q)" docs/adr/*.md 2>/dev/null)"; \
	if [ -z "$$hits" ]; then \
		echo "  (no filename/content match, try a narrower term)"; \
	else \
		for f in $$hits; do \
			status="$$(grep -m1 '^Status:' "$$f" | sed 's/^Status: *//')"; \
			echo "  $$f  [$${status:-unknown}]"; \
		done; \
	fi
```

```yaml
# Taskfile.yml (go-task)
tasks:
  adr-find:
    desc: "Find ADRs and related code for a concept: task adr-find -- <concept>"
    cmds:
      - |
        Q="{{.CLI_ARGS}}"
        test -n "$Q" || { echo 'Usage: task adr-find -- "concept"'; exit 1; }
        echo "--- docs/adr matching \"$Q\" ---"
        hits="$(grep -ril -- "$Q" docs/adr/*.md 2>/dev/null)"
        if [ -z "$hits" ]; then
          echo "  (no filename/content match, try a narrower term)"
        else
          for f in $hits; do
            status="$(grep -m1 '^Status:' "$f" | sed 's/^Status: *//')"
            echo "  $f  [${status:-unknown}]"
          done
        fi
```

- **`docs/adr/adr-reference.sgrule.yml`** (optional, only if the repo uses
  ast-grep, and only if the codebase actually annotates comments with
  `ADR-<id>` to point back at a decision — this rule finds such comments,
  it doesn't create the convention of writing them) — a discovery-only
  rule, not a lint gate, so it must not live under a directory the repo's
  default ast-grep scan picks up. Set `language:` to the repo's actual
  primary language (check `ast-grep --help` or existing `.sgrule.yml` files
  in the repo for valid values — don't leave it as `go` in a non-Go repo):

```yaml
id: adr-reference
language: go   # set to this repo's actual primary language
severity: info
message: "Code comment references an ADR. Cross-check docs/adr/ (the adr-find task)."
rule:
  kind: comment
  regex: 'ADR-\d{4,}'
note: "Discovery helper for `make adr-find`, not a lint gate."
```

## Template for a repo's first ADR

When `docs/adr/` doesn't exist yet, its first file records this convention
itself, dated the day it's added:

```markdown
# <id>. Record architecture decisions

Date: YYYY-MM-DD
Status: Accepted

## Context

- Design decisions and their tradeoffs live only in commit messages, issues,
  and chat history today. Issues close and get buried, chat isn't durable
  project state, and a commit message is easy to miss skimming `git log`.

## Decision

- Record significant architecture/infra decisions as ADRs under `docs/adr/`,
  one file per decision, id `docs/adr/DDMMYYHHMM-slug.md`, immutable once
  accepted. A reversal is a new ADR that supersedes the old one.
- No shared index file — the directory listing plus an `adr-find` task
  (or a plain `grep`) is the index.
- `Status:` is always an inline line, never a heading.
- Sections: Context, Decision, Rejected (when relevant), Consequences.
  Bullets, not prose. No issue numbers in the body.

## Consequences

- Adds one file per real decision, maintained by hand, no CI enforcement.
- `adr-find` (or a plain `grep`) is the only way to check status; nothing
  else needs to stay in sync.
```

## Checklist before calling it done

- [ ] Filename is `docs/adr/DDMMYYHHMM-slug.md`, timestamp taken at creation
      (even if `Date:` reflects an earlier, retroactive decision date).
- [ ] `Status:` is inline, not a heading, and is exactly `Proposed` or
      `Accepted` — no other value.
- [ ] No issue/ticket numbers anywhere in the body.
- [ ] If this supersedes another ADR, the old file is untouched — its
      `Status:` line does NOT change — and the new file names the old id
      explicitly in its Decision section.
- [ ] Nothing written to `docs/adr/index.md` (don't create it).
- [ ] Bullets, not paragraphs, in every section.
