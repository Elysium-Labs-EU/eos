# 0004. Sink plugin protocol version negotiation: tolerant, not strict

Date: 2026-08-10
Status: Accepted

## Context

`sinkAwaitReady` accepted exactly the literal line `READY` and eos told the
plugin nothing about which wire format it speaks. Sink plugins are released
independently of eos (`eos-sink-<name>/vX.Y.Z`, each in its own repo), and
`exec:` lets a sink entry point at any private binary eos has never seen — so
there is no lockstep upgrade to rely on. When the record format changes, a
mismatched pair has no way to say so: the old plugin accepts `READY`, starts
fine, then either silently misreads records or dies mid-stream with an error
that points nowhere near the real cause.

## Decision

- eos exports `EOS_SINK_PROTOCOL_VERSION=<n>` alongside the existing
  `EOS_SINK_*` vars, so a plugin can adapt or refuse before sending `READY`.
- The `READY` line may carry a version token: `READY` (bare) or `READY <n>`.
  Everything after the version token is reserved and ignored.
- A bare `READY` means version 1, the format shipping today. It is never an
  error.
- Declared version `<=` what eos speaks: accept, negotiate the lower of the
  two.
- Declared version `>` what eos speaks: refuse, kill the plugin, and name
  both versions in the error, telling the reader to upgrade eos.
- Declared version not a number: refuse the same way, quoting what was sent.

## Rejected

- **Strict equality** (refuse any plugin whose version isn't exactly eos's):
  rejects every plugin that exists today, including all four first-party
  ones, plus every private `exec:` binary. It also refuses a *newer* plugin
  talking to an *older* eos — the wrong direction to fail. eos can always
  serve an older plugin and can never serve a newer one; the asymmetry is
  deliberate.
- **No negotiation mechanism until a second version ships**: a version
  constant with nothing comparing against it is a comment, not a mechanism.
  Building the compare-and-refuse path now means the day a v2 format ships,
  only the constant changes — not the handshake code, and not every plugin
  in the field.

## Consequences

No existing plugin needs to change: the bare `READY` path is permanent, not
a migration step. A plugin that wants to reject an incompatible eos, or
speak a newer format only when eos supports it, now has a signal to act on
before the first record is written. The handshake is a published contract:
`PROTOCOL.md` in the `eos-plugins` repo is where plugin authors read it, and
it is updated separately, in that repo, so a plugin author never has to read
eos source to learn the handshake.
