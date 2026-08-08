# Security Policy

## Reporting a Vulnerability

Report security vulnerabilities privately through GitHub's [Private Vulnerability Reporting](https://github.com/Elysium-Labs-EU/eos/security/advisories/new). This opens a private advisory visible only to you and the maintainers; nothing is posted publicly until a fix is ready.

Use this channel for anything that could be exploited if disclosed early: a way to escalate privileges through a service supervised by eos, a way to bypass log/env redaction and leak a secret, a daemon-socket or IPC issue that lets an unauthorized local user control eos, or similar.

We aim to acknowledge new reports within a few days and will work with you on a disclosure timeline once the issue is confirmed.

## Everything Else

Bug reports, crashes, and general "this doesn't work as documented" issues are not security reports and belong in a [public issue](https://github.com/Elysium-Labs-EU/eos/issues/new/choose) — the normal way to get eyes on them quickly.

If a bug needs supporting evidence, run `eos diagnose` first. It bundles version, daemon, and per-service status plus recent logs into a privacy-safe `tar.gz` you can attach directly: output is allowlisted, hostnames are hashed, and log lines are scrubbed for secrets before anything is written. Only `eos diagnose --include-env` bypasses this (it dumps raw env files on purpose) — never attach that output to a public issue. If you genuinely need to hand over something unredacted (an `--include-env` bundle, a raw log with something sensitive in it), use the private vulnerability report above rather than posting it publicly.
