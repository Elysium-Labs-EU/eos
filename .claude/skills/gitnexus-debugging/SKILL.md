---
name: gitnexus-debugging
description: "Use when the user is debugging a bug, tracing an error, or asking why something fails. Examples: \"Why is X failing?\", \"Where does this error come from?\", \"Trace this bug\""
---

# Debugging with GitNexus

## When to Use

- "Why is this function failing?"
- "Trace where this error comes from"
- "Who calls this method?"
- "This endpoint returns 500"
- Investigating bugs, errors, or unexpected behavior

## Bind the repository first

A root cause traced in the wrong repository is a wrong root cause.

Call `list_repos {}` before the first tool call. With one indexed repository,
use the examples below as written. With more than one, pass `repo` on every
call: an omitted `repo` normally errors, but under an MCP policy with a
configured default it resolves to that default silently. If you cannot tell
which repository is meant, stop and ask. This matters most for `cypher`, whose
statement carries no in-band hint of which database it ran against.

`list_repos` is paginated, so page with `offset: pagination.nextOffset` until
`hasMore` is false before concluding a repository is absent.

A stale index describes the code from before your bug, so refresh before
trusting a trace, and state the repository and index freshness with the
diagnosis.

## Workflow

```
0. list_repos {}                                          , then  Bind repo
1. query({search_query: "<error or symptom>"})            , then  Find related execution flows
2. context({name: "<suspect>"})                    , then  See callers/callees/processes
3. READ gitnexus://repo/{name}/process/{name}                , then  Trace execution flow
4. cypher({statement: "MATCH path..."})                 , then  Custom traces if needed
```

> If "Index is stale", run `node .gitnexus/run.cjs analyze` in terminal.

## Checklist

```
- [ ] list_repos {} — bind repo; explicit repo when >1 indexed, ask if ambiguous
- [ ] Understand the symptom (error message, unexpected behavior)
- [ ] query for error text or related code
- [ ] Identify the suspect function from returned processes
- [ ] context to see callers and callees
- [ ] Trace execution flow via process resource if applicable
- [ ] cypher for custom call chain traces if needed
- [ ] Read source files to confirm root cause
- [ ] State the repository and index freshness with the diagnosis
```

## Debugging Patterns

| Symptom              | GitNexus Approach                                          |
| -------------------- | ---------------------------------------------------------- |
| Error message        | `query` for error text, then `context` on throw sites |
| Wrong return value   | `context` on the function, then trace callees for data flow    |
| Intermittent failure | `context`, then look for external calls, async deps            |
| Performance issue    | `context`, then find symbols with many callers (hot paths)     |
| Recent regression    | `detect_changes` to see what your changes affect — pass `worktree` for a linked worktree |
| "How does A reach B?" | `trace` between the two symbols — shortest call chain in one call |

## Tools

**query** — find code related to error:

```
query({search_query: "payment validation error", repo: "my-app"})
, then  Processes: CheckoutFlow, ErrorHandling
, then  Symbols: validatePayment, handlePaymentError, PaymentException
```

**context** — full context for a suspect:

```
context({name: "validatePayment", repo: "my-app"})
, then  Incoming calls: processCheckout, webhookHandler
, then  Outgoing calls: verifyCard, fetchRates (external API!)
, then  Processes: CheckoutFlow (step 3/7)
```

**cypher** — custom call chain traces. Pass `repo` alongside the statement; the
Cypher text itself names no repository, so the result is unattributable without
it:

```cypher
MATCH path = (a)-[:CodeRelation {type: 'CALLS'}*1..2]->(b:Function {name: "validatePayment"})
RETURN [n IN nodes(path) | n.name] AS chain
```

**trace** — shortest call chain between two symbols ("how does A reach B?"), one call instead of chaining `context` hops:

```
trace({ from: "processCheckout", to: "fetchRates", repo: "my-app" })
, then  status: ok, hopCount: 3
, then  hops: processCheckout , then  validatePayment , then  verifyCard , then  fetchRates
, then  edges: CALLS (1.0), CALLS (0.95), CALLS (1.0)
```

When no path exists, `trace` reports the furthest reachable node — exactly where the chain breaks (dynamic dispatch, reflection, or an external boundary).

## Example: "Payment endpoint returns 500 intermittently"

```
0. list_repos {}
   , then  total: 2 (my-app, billing-api) — bind my-app explicitly on every call

1. query({search_query: "payment error handling", repo: "my-app"})
   , then  Processes: CheckoutFlow, ErrorHandling
   , then  Symbols: validatePayment, handlePaymentError

2. context({name: "validatePayment", repo: "my-app"})
   , then  Outgoing calls: verifyCard, fetchRates (external API!)

3. READ gitnexus://repo/my-app/process/CheckoutFlow
   , then  Step 3: validatePayment , then  calls fetchRates (external)

4. Root cause: fetchRates calls external API without proper timeout
   Repository: my-app  Index: current
```

With a single indexed repository, step 0 returns `total: 1` and the `repo`
argument drops out of every call above.
