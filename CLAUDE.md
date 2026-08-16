# Make Commands — Always Use These

**Never run raw `go test`, `golangci-lint run`, `go build`, etc. Always use make.**

Run `make help` to list all available targets and find the right one for your task. The list may change — always check before reaching for a raw command.

Before any commit or PR: `make ci`. If lint fails: `make fix`, then retry.

---

# Code Style

See [style.md](STYLE.md) — Go-FP + data-oriented design guide.

---

# Architecture Decisions

Before changing established design (daemon lifecycle, CI gates, test harness, config/permission model): check `docs/adr/` first. Run `make adr-find Q="concept"` to find the relevant ADR plus related code (ast-grep scan for existing ADR references, GitNexus for related symbols). The `docs/adr/` listing is the full set, and `make adr-find` prints each match with its status.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **eos** (5116 symbols, 26367 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/eos/context` | Codebase overview, check index freshness |
| `gitnexus://repo/eos/clusters` | All functional areas |
| `gitnexus://repo/eos/processes` | All execution flows |
| `gitnexus://repo/eos/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

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
