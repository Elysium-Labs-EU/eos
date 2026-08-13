# 0011. Drop the shared ADR index

Date: 2026-08-13
Status: Accepted

## Context

- 0001 established `docs/adr/index.md`: a hand-maintained table of every
  ADR's number, title, and status, updated by every new ADR alongside its
  own file.
- Two branches adding an ADR concurrently each append a row after the same
  anchor line. Git cannot order two insertions at the same point, so every
  pair of concurrent ADRs conflicts in the index regardless of numbering,
  independent of whether the two branches picked the same number.
- The status column carries no signal today: every existing ADR reads
  Accepted.
- Nothing references an ADR by number: zero `ADR-NNNN` matches in Go code,
  zero cross-ADR references, zero references anywhere outside `docs/adr/`.
  `adr-reference.sgrule.yml` exists specifically to find such references
  and finds none.

## Decision

- Delete `docs/adr/index.md`. The directory listing is the index.
- Extend `make adr-find` to print each matching file's `Status:` line next
  to its path, so the one column with any real structure survives as a
  query instead of a maintained file.
- Sequential numbering stays as-is. A gap left by an abandoned branch is
  normal, not a defect to repair; renumbering an accepted ADR is worse than
  a gap, since the number is how anything that ever does reference an ADR
  would do so.

## Rejected

- **Generate `index.md` from the directory in CI and gate on staleness**:
  removes the conflict, but a generator plus a permanent staleness check is
  ongoing machinery to maintain a table that nothing reads.
- **Date-prefixed filenames instead of sequential numbers**: removes the
  filename collision, but that collision was already the cheap half — git
  reports it as a plain add/add conflict and a rename fixes it. The index
  conflict is the expensive half: it fires on every pair of concurrent ADRs
  regardless of naming scheme, because both sides still append at the same
  anchor. Dropping the index removes that; renaming files does not.
- **Leave it and absorb the cost**: defensible only while ADRs are rare and
  written serially, not the normal mode here.

## Consequences

- Concurrent ADR branches now only ever collide on filename: a loud add/add
  conflict git reports plainly, fixed by a rename.
- `make adr-find Q="..."` becomes the only way to see an ADR's status; no
  standing file can fall out of date.
- Supersedes the index-discoverability half of 0001's Consequences section;
  0001's numbering, immutability, and file format still stand.
