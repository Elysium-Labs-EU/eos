# Contributing to eos

## Prerequisites

Go 1.26.4 or later and `make` are required. Verify with `go version` and `make --version`.

## Setup

```bash
git clone https://codeberg.org/Elysium_Labs/eos
cd eos
make setup
```

`make setup` installs the development toolchain (golangci-lint, lefthook, sg). Run `make help` to see all available targets; always prefer a make target over raw `go` or tool commands.

## Making Changes

Before touching any function or method, read [STYLE.md](STYLE.md) for the coding conventions that apply to all changes.

Open an issue before starting work on a non-trivial change. This avoids duplicate effort and makes sure the direction fits the project. Small fixes and documentation improvements can go straight to a PR.

Branch from `main` and name the branch after the change: `feat/service-labels`, `fix/restart-backoff`, `test/daemon-lifecycle`.

## Running Tests

```bash
make ci
```

This runs the full lint and test suite. It must pass before opening a PR. If lint reports violations, `make fix` resolves most of them automatically; run `make ci` again after.

Some tests require a Linux environment. Use `make test-linux` to run them in the OrbStack Debian VM, or open your PR and let CI handle it.

## Commit Format

eos uses [Conventional Commits](https://www.conventionalcommits.org). The prefix determines which section of the changelog the commit appears in.

```
feat: add per-service log retention config
fix: clamp restart backoff to configured max
test: cover daemon shutdown under ctx cancel
refactor: extract systemd unit builder to pure func
docs: document EOS_VERBOSE env variable
chore: bump golangci-lint to v1.60
```

Breaking changes go in the commit footer: `BREAKING CHANGE: <description>`.

## Opening a Pull Request

Fill in the PR template. The summary should explain *why* the change is needed, not just what it does. Link the issue it resolves with `Closes #N`.

All CI checks must be green. A PR that breaks `make ci` will not be reviewed until it is fixed.
