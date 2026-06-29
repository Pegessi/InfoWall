# 2026-06-29 — CLI optimization: agent-friendly + markdown upload

## What changed

Made the `infowall` CLI scriptable/agent-friendly and able to upload complex
markdown from files instead of forcing content onto the command line.

- **Batch markdown upload (`push`)**: now accepts any number of arguments, each
  a file or `-`/stdin. Each document is pushed as a separate item. A bad source
  is reported but does not abort the batch; the command exits non-zero if any
  source failed. (Directory expansion was prototyped and then dropped at
  reviewer request — see below; push whole folders with a shell glob like
  `notes/*.md` instead.)
- **`--json` everywhere**: `push` emits an array of per-source result objects;
  `list` / `get` / `pin` / `unpin` echo the server JSON; `delete` emits
  `{"id","deleted"}`. In `--json` mode, failures print `{"error":...}` to
  stderr and stdout stays clean.
- **Consistent flags + exit codes**: `--server` / `--api-key` / `--json` are
  shared across all server-talking subcommands via `addClientFlags`; `pin` /
  `unpin` previously read only env vars and now take these flags too. Flags may
  appear before or after positional args (a small permuting parser, since
  Go's `flag` stops at the first positional).
- **New `get <id>` command** backed by a new authenticated
  `GET /api/items/{id}` route (`?raw=1` includes the markdown source). The
  route reuses `store.Get` and maps `ErrNotFound` → 404.
- **Non-interactive automation**: `push` with arguments and `delete --json`
  never prompt.

## Key files

- `cmd/infowall/main.go` — full CLI rewrite around a `clientConfig` helper.
- `cmd/infowall/main_test.go` — new tests: source expansion (files/dirs/stdin,
  partial failure), frontmatter injection, flag permutation, push end-to-end
  via `httptest`, error formatting.
- `internal/server/server.go` — `handleGetItem` + route registration.
- `README.md`, `CLAUDE.md` / `AGENTS.md` — documented the above.

## Design notes / pitfalls

- **`flag` stops at the first positional.** `push a.md b.md --json` would treat
  `--json` as a path. `parseFlags` permutes flags ahead of positionals and
  appends a `--` terminator so filenames beginning with `-` still work after it.
- **Batch resilience.** `expandSources` attaches per-argument errors to the
  `source` (surfaced at `read()`) rather than returning early, so one missing
  file doesn't drop the rest of the batch.
- **Directory expansion dropped (reviewer request).** An earlier iteration let a
  directory argument expand to its top-level `*.md`/`*.markdown` files. The
  reviewer judged it unnecessary, so directories are now rejected as a
  per-source error; folders are pushed with a shell glob (`push notes/*.md`).
  The shell already does the expansion, so the CLI stays simpler.
- **Out of scope (left untouched):** the in-progress `显示优化` frontend changes,
  auth model, schema, and any browser upload UI. The pre-existing "first-8-hex
  prefix" id-matching claim in the docs is inaccurate (store matches exact ids)
  but predates this work; reworded the sections rewritten here to not repeat it.

## Validation

`go build ./...`, `go vet ./...`, `gofmt -l` (clean for touched files),
`go test ./...` (incl. new CLI tests), `make build`, plus manual end-to-end:
multi-file + stdin push, directory arg rejected, `--json` output, `get`
(human/`--json`/`--raw`), pin/unpin/delete, 404 → exit 1, API-key gating on the
new route (401 without key, 200 with).
