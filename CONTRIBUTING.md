# Contributing to infowall

Thanks for working on infowall. This is a small, single-binary project; the
process is deliberately lightweight. Read [`AGENTS.md`](AGENTS.md) /
[`CLAUDE.md`](CLAUDE.md) first — they are the canonical guide to the stack,
commands, layout, coding conventions, and pitfalls. This file only adds the
human-contributor process around them.

## Prerequisites

- **Go 1.22+** (no C toolchain — SQLite is the pure-Go `modernc.org/sqlite`).
- **Node 20+** with npm (only to build the frontend).

## Getting started

```bash
# Build the production binary (frontend + embedded Go binary)
make build

# Run it
./bin/infowall serve --addr :8899 --db infowall.db

# Or develop with live reload (two terminals):
make web-dev                                   # terminal 1: Vite on :5173
go run -tags dev ./cmd/infowall serve --dev    # terminal 2: Go server, proxies / to Vite
```

## Before you open a change

Run the validation that matches what you touched:

- **Go**: `gofmt -l .` (must print nothing), `go vet ./...`, `go test ./...`.
- **Frontend**: from `web/`, `npm run lint` (type-check) and `npm run build`.
- **End-to-end**: `make build`, then exercise the change via
  `./bin/infowall push` or the browser when behavior changed.

## Conventions

- Follow the coding conventions in [`AGENTS.md`](AGENTS.md): `gofmt`/package-doc
  comments and wrapped errors on the Go side; `strict` TypeScript, the `@/`
  import alias, and the renderer registry on the frontend side.
- Adding a content type touches four places together — Go model constants, the
  parser, the TS `Item` type, and a registered renderer. See the
  [Common Edit Areas](AGENTS.md#common-edit-areas) table.
- Update [`README.md`](README.md) when user-facing behavior changes (CLI flags,
  HTTP API, content types, environment variables).
- If the project is under git, use a feature branch and conventional commit
  messages (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). Do not
  commit build artifacts or `*.db` files — they are git-ignored.

## Working logs

For non-trivial design work, add a short log under
`docs/working-logs/YYYY-MM-DD-topic.md` describing what changed, why, the key
decisions, and any pitfalls. Keep the root agent files (`AGENTS.md` /
`CLAUDE.md`) short; push detail into working logs.
