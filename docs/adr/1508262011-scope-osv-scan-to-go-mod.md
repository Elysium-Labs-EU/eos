# 1508262011. Scope the OSV scan to go.mod, don't recurse

Date: 2026-08-15
Status: Accepted

## Context

- `osv-scan.yml` ran `osv-scanner scan --lockfile=go.mod --recursive .`. The
  `--recursive .` walk covers the whole tree, including every npm/pnpm/bun
  lockfile staged under `testdata/fixtures/` to give the supervision tests a
  real Node app to spawn.
- Those fixture lockfiles are pinned to whatever version each fixture needs
  at the time it was added; they are not eos's supply chain and nothing in
  them ships. A fixture's own upstream advisories still fail the check.
- `go.mod` is eos's only real dependency manifest. No other manifest exists
  outside `testdata/fixtures/` in this repo today.

## Decision

- Drop `--recursive .`; run `osv-scanner scan --lockfile=go.mod` only.
  With no other real manifest in the tree, recursion has nothing left to
  find beyond what the explicit `--lockfile` flag already covers.
- Add a test asserting the flag stays gone, so a later PR can't silently
  reintroduce the tree walk.

## Rejected

- **Keep `--recursive` and add a path-based ignore for `testdata/`**: works
  today, but is a list that has to be remembered and extended every time a
  new fixture directory is added under `testdata/fixtures/`; a forgotten
  entry silently reopens the exact failure mode this fix closes. Scoping the
  scan needs no such list, because there is nothing else in the tree for
  recursion to find.
- **Bump the fixture lockfiles to clear the current findings**: only masks
  today's advisories; the next upstream npm advisory against any fixture
  dependency re-breaks the check, forever, for versions pinned to what a
  fixture needs rather than to eos's supply chain.

## Consequences

- A real Go dependency advisory (a finding against `go.mod` itself) still
  fails the check; verified locally against the pinned scanner binary.
- A new fixture added later under `testdata/fixtures/` cannot affect this
  check, regardless of what lockfile format it ships.
- If eos ever gains a second real manifest outside `testdata/`, this scan
  needs an explicit path added, not a blanket `--recursive` restored.
