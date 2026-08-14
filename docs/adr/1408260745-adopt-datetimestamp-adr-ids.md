# 1408260745. Adopt datetimestamp ADR ids

Date: 2026-08-14
Status: Accepted

## Context

- 0001 numbers ADRs sequentially. Two branches authoring an ADR in parallel
  pick the same next number and collide on an add/add conflict.
- 0011 already left this repo with a numbering gap (0007 is missing) after
  an abandoned branch claimed it, and treated the gap as normal rather than
  a defect to repair.
- Parallel ADR authorship by independent workers is the normal mode here,
  not the exception.
- The sibling argus repo already adopted a datetimestamp id scheme for the
  same reason; this ADR mirrors that decision for eos.

## Decision

- ADR ids are minute-precision datetimestamps of the form DDMMYYHHMM (day,
  month, two-digit year, hour, minute, 24h), e.g.
  `docs/adr/1408260745-slug.md`.
- The id self-allocates from creation time, so two workers authoring ADRs
  in parallel never collide on id.
- This supersedes the sequential-numbering clause of 0001's Decision
  section. 0001's other clauses (one file per decision, immutable once
  accepted, the Context/Decision/Rejected/Consequences format) still stand.

## Rejected

- **Keep sequential numbers**: collides under parallel authorship, which is
  the normal mode here, not an edge case.
- **An allocator or lockfile**: more machinery than a self-allocating
  timestamp needs.
- **Second-precision ids**: a same-minute collision between two ADRs is
  vanishingly rare, and even then costs only a rename.

## Consequences

- Existing ADRs 0001 through 0011 keep their sequential ids and are never
  renamed; accepted ADRs are immutable per 0001.
- New ADRs use the datetimestamp scheme; the two id shapes coexist in
  `docs/adr/` indefinitely.
- Ids sort chronologically: every existing 4-digit sequential id sorts
  before any 10-digit datetimestamp id in a plain lexicographic listing.
- `adr-reference.sgrule.yml`'s regex widens from `ADR-\d{4}` to
  `ADR-\d{4,}` so a comment referencing a datetimestamp id is still caught
  by `make adr-find`.
