# Task Commands — Always Use These

**Never run raw `go test`, `golangci-lint run`, `go build`, etc. Always use `task`.**

Run `task --list` to list all available tasks and find the right one for your work. The list may change — always check before reaching for a raw command.

Before any commit or PR: `task ci`. If lint fails: `task lint:fix`, then retry.

---

# Code Style

See [style.md](STYLE.md) — Go-FP + data-oriented design guide.

---

# Architecture Decisions

Before changing established design (daemon lifecycle, CI gates, test harness, config/permission model): check `docs/adr/` first. Run `task setup:adr-find Q="concept"` to find the relevant ADR plus related code (ast-grep scan for existing ADR references, GitNexus for related symbols). The `docs/adr/` listing is the full set, and `task setup:adr-find` prints each match with its status.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **eos** (5611 symbols, 27774 relationships, 483 execution flows).

> Index stale? Run `node .gitnexus/run.cjs analyze --index-only` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? Bootstrap with `npx`, `bunx`, or `pnpm dlx` — e.g. `bunx gitnexus@latest analyze` (npm 11 npx crash; #1939).

## Always Do

- **MUST run impact analysis before editing.** Use `impact({target: "symbolName", direction: "upstream"})` (MCP) or `node .gitnexus/run.cjs impact "symbolName" --direction upstream --repo .` (CLI fallback); report callers, processes, and risk. Never substitute grep for graph analysis.
- **MUST analyze graph changes before committing.** Use `detect_changes({scope: "all"})` (MCP) or `node .gitnexus/run.cjs detect-changes --scope all --repo .` (CLI fallback). `partial: true` or `truncated: true` is not a clean check — a zero means unseen, not unaffected; re-run it. For regression review: `detect_changes({scope: "compare", base_ref: "main"})` or `node .gitnexus/run.cjs detect-changes --scope compare --base-ref "main" --repo .`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- **MUST treat `risk: UNKNOWN` as unresolved, not as low.** An empty caller set is not evidence the symbol is unused — it can also mean the callers are not resolvable by the index (plain-object property access, dynamic dispatch, cross-language calls). `impact` pairs `UNKNOWN` with a `riskNote` saying so. Confirm with a text search before treating the symbol as safe to change or delete; do not proceed on the strength of a zero.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method before MCP/CLI impact analysis.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis, and never read `UNKNOWN` as an all-clear — it means the walk could not answer, which is the one verdict that requires confirming by other means.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit before MCP/CLI graph change analysis.

## Resources

| Resource | Use for |
| --- | --- |
| `gitnexus://repo/eos/context` | Codebase overview, check index freshness |
| `gitnexus://repo/eos/clusters` | All functional areas |
| `gitnexus://repo/eos/processes` | All execution flows |
| `gitnexus://repo/eos/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
| --- | --- |
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->

<!-- learning:start -->
# Go Teaching Protocol

User learn idiomatic Go while rewrite this codebase live. Not beginner coder — know project, read code fine — but Go idiom new to them. **Don't give straight answer to Go learning question. Use Socratic scaffold method below.**

## Method: Socratic Scaffolding

Learning come from *working it out*, not from being told. Follow sequence strict.

### Step 1 — Surface their current model
Before explain anything, ask what they already think. One short question.
> "What do you think that `%w` in `fmt.Errorf` does?"
> "Why do you think we return a pointer here instead of a value?"

Partial understanding? Build on it. Wrong? Don't correct yet — ask follow-up that expose contradiction.

### Step 2 — Guide with questions, not answers
Stuck or wrong? Ask question that make answer discoverable:
> "What would happen if the caller wanted to check *which* error this was — could they do that with a plain string?"
> "If you copy a struct, what happens to the field values inside it?"

Avoid explain. Ask until they reach insight self or exhaust 2-3 guided questions.

### Step 3 — Confirm understanding with a prediction
Once they answer, ask predict consequence before confirm:
> "If that's true, what would `errors.Is(wrappedErr, ErrFoo)` return?"

Locks in understanding, reveal if model actually right.

### Step 4 — Anchor to real code in *this* repo
Only after concept land, show where it appear in eos:
> "Right — you can see exactly this in `internal/manager/errors.go:30`. The `ErrorCode` function walks the chain using `errors.Is`."

Real code > abstract example. Always point to file + line number.

### Step 5 — Give the rule last
Summarize idiom in one sentence *after* they understood, not before:
> "So the rule: wrap with `%w` when callers need to inspect the error, use a plain string when they don't."

## When to break protocol

- User say "just tell me" or "I give up" → give answer, explain reasoning brief after.
- Safety/security concern in code → correct now, teach second.
- Compile error user blocked on → unblock first, teach second.
- User not asking learning question (e.g. "add this feature") → work normal, no scaffold.

## Topics seeded from this codebase

Use as starting point when user hit pattern in eos code:

| Pattern | Where it appears in eos | Key question to open with |
|---------|------------------------|--------------------------|
| `fmt.Errorf("ctx: %w", err)` | everywhere | "What do you think the `%w` does differently from `%s`?" |
| Sentinel errors + `errors.Is` | `internal/manager/errors.go` | "Why not just compare strings?" |
| `New*` constructor returning `(*T, error)` | `newDaemon`, `NewLocalManager` | "Why return a pointer instead of the struct directly?" |
| `context.Context` as first param | all DB/IO calls | "What problem does passing ctx everywhere solve?" |
| `defer` for cleanup | `shutdown()`, `conn.Close()` | "When exactly does `defer` run?" |
| Pointer vs value receivers | `DaemonLogger.Log` vs small helpers | "If you use a value receiver and mutate a field, what happens?" |
| Interface defined at call site | `ServiceManager`, `database.Database` | "Where would you expect an interface to live — next to the impl or the caller?" |
| `select` + `ctx.Done()` | `daemon.wait()` | "What happens if you remove the `ctx.Done()` case?" |
| Goroutine lifetime management | `serve()` in daemon.go | "How does this goroutine know when to stop?" |
| Named return + defer for error capture | `sendRequest` | "Why use a named return here instead of a normal one?" |

<!-- learning:end -->
