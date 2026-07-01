# 2026-07-01 — Local SQLite persistence: db info / backup + safe path handling

## Goal

Make local SQLite persistence operationally trustworthy for a single-machine
"formal operation" run: operators/agents can verify the active DB target, take a
safe backup of the live DB without prompts, and follow a documented runbook.
Storage itself (SQLite via `modernc.org/sqlite`, WAL mode) was already in place —
this slice adds the operational surface around it.

## What changed

- **`internal/store/store.go`**
  - `EnsureParentDir(path)` — creates the parent directory of a DB path
    (`MkdirAll`) or returns a clear, path-contextual error (target is a
    directory, parent component is a file, empty path). SQLite special targets
    (`:memory:`, `file:` DSNs) pass through untouched. Called from `Open`, so
    `serve --db nested/dir/x.db` now creates `nested/dir` deliberately instead of
    failing obscurely — and there is no silent temp/in-memory fallback.
  - `Count(ctx)` — item row count for status reporting.
  - `Backup(ctx, outPath)` — consistent snapshot via SQLite `VACUUM INTO`. Safe
    against a live WAL database. Refuses to overwrite an existing target and
    creates the destination parent dir.
- **`internal/server/server.go`** — `New` wraps the `store.Open` error with the
  DB path for a clearer startup failure.
- **`cmd/infowall/main.go`** — new local `db` command group:
  - `db info [--db PATH] [--json]` — absolute path, exists, size, WAL/SHM
    sidecars, item count, initialized flag, api-key-set flag.
  - `db backup --out PATH [--db PATH] [--json]` — VACUUM INTO backup with size
    report.
  - Both operate on the **file** (via `--db` / `INFOWALL_DB`), not over HTTP, so
    they work whether or not a server is running. `failLocal` mirrors the
    existing `clientConfig.fail` contract: JSON `{"error":...}` to stderr in
    `--json` mode, non-zero exit on any failure.
- **Tests** — `internal/store/store_test.go` (EnsureParentDir cases, nested
  Open, Count, Backup integrity + refuse-overwrite) and new cases in
  `cmd/infowall/main_test.go` (db info JSON, backup JSON + refuse-overwrite,
  missing `--out`, unknown subcommand).
- **Docs** — README gains a "Formal local operation: data persistence & backup"
  runbook (explicit `--db`, API-key note, db info/backup, restore-by-copy with
  WAL sidecar guidance, explicit local-only scope) plus `db` entries in the CLI
  reference and the env-var table. CLAUDE.md/AGENTS.md updated identically
  (subcommand list + a Common Edit Areas row).

## Key decisions / pitfalls

- **VACUUM INTO, not `cp`.** WAL is enabled in `store.Open`, so a raw file copy
  of `infowall.db` can miss un-checkpointed pages in the `-wal` sidecar and yield
  a torn backup. `VACUUM INTO` takes a read transaction and writes a
  fully-checkpointed, self-contained copy (no sidecars) — safe while serving.
  Verified `VACUUM INTO ?` (bound param) works on `modernc.org/sqlite` v1.34.5.
- **Refuse-overwrite by design.** `VACUUM INTO` itself errors on an existing
  file; we also pre-check and return a friendly message, so a timestamped `--out`
  is the intended usage and no backup is ever clobbered.
- **`db info` initializes the schema.** Opening the store runs
  `CREATE TABLE IF NOT EXISTS`, matching `serve`. So `db info` on a brand-new
  path reports `exists:true, initialized:true, items:0` rather than "missing".
  This is intentional (it confirms the path is usable) and documented.
- **No `db restore` command.** Per scope, restore is a documented manual
  copy-into-place (stop server → move sidecars aside → copy backup → restart),
  not a destructive CLI command.

## Out of scope (untouched)

No cloud/remote/offsite backup, no accounts, no multi-device model, no
scheduled/rotating backups, no compression, no frontend changes (the pending
responsive-layout task was not touched; no overlap — this is backend/CLI/docs).

## Validation

`gofmt -l` clean (touched files), `go vet ./...` clean, `go test ./...` pass
(new store + CLI tests), `make build` succeeds. Manual e2e: serve/`db` on a
nested path (parent auto-created); `db info --json` empty vs populated; `db
backup --json` then reopened the backup and confirmed matching row count;
re-backup to same path → non-zero exit + JSON error on stderr.
