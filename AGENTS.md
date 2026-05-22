# AGENTS.md

## Purpose

`gitcrawl` is a local-first GitHub archive CLI. Preserve the read-only archive
model: inspect local stores, API-derived snapshots, databases, and generated
indexes without mutating live GitHub state unless a command explicitly owns that
behavior.

Reusable archive mechanics belong in `crawlkit`. Keep GitHub-specific parsing,
metadata, auth discovery, and CLI behavior in this repository.

## Development Rules

- Do not write to real user archive stores in tests.
- Use temp directories and temp SQLite databases for test state.
- Do not print tokens, private issue bodies, private comments, emails, or
  decrypted key material from diagnostics.
- Keep CLI output explicit about partial coverage, stale mirrors, missing
  caches, and unavailable local state.
- Prefer small Go stdlib-first changes unless a dependency is already part of
  the repo contract.

## Validation

Run before handoff:

```bash
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off go vet ./...
GOWORK=off go test -count=1 ./...
```
