# 0001. Record architecture decisions

Date: 2026-08-09
Status: Accepted

## Context

Design decisions and their tradeoffs lived only in GitHub issues, chat history and memory. Issues close and get buried, chat and memory are not durable, reviewable project state.

## Decision

Record significant architecture decisions as ADRs in `docs/adr/`. One file per decision, sequentially numbered, immutable once accepted. A reversal gets a new ADR that supersedes the old one. The old file is never edited or deleted.

## Format

Sections: Context, Decision, Rejected (alternatives), Consequences. Bullets, not prose. Target 15 to 30 lines per file. No issue numbers in the body: issues get renumbered, closed, or migrated between trackers; ADRs are the permanent record.

## Consequences

Adds one file per real decision, maintained by hand, no CI enforcement yet. Discoverability: `docs/adr/index.md` lists every ADR by title and status; `make adr-find Q="..."` cross-references ADR docs against related code via ast-grep and GitNexus.
