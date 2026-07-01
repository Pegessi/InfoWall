# 2026-07-01 — SQLite schema version & migration guard

## Goal

Harden local SQLite persistence for formal operation: give the database an
explicit schema version so future releases can evolve the schema safely, without
silently mutating or downgrading existing data.

## What changed

- **`internal/store/store.go`**
  - Added `schemaVersion = 1` constant (the version this binary understands,
    tracked via SQLite `PRAGMA user_version`).
  - Added a centralized `migrate(db)` called from `Open()` (replacing the inline
    `db.Exec(schema)`). Behavior:
    - `user_version == schemaVersion` → no-op (idempotent).
    - `user_version > schemaVersion` → return a clear contextual error naming
      found vs supported version **and the DB is left unchanged** (no downgrade,
      no data mutation); `Open` returns a nil store.
    - `user_version < schemaVersion` → run ordered steps via `migrateStep`, each
      followed by stamping `PRAGMA user_version`.
  - `migrateStep(db, from)` holds the per-version changes. The only step so far,
    `0 -> 1`, applies the baseline schema (`CREATE IF NOT EXISTS`) and stamps v1.
- **`internal/store/store_test.go`** — three focused tests:
  - fresh DB → `user_version == schemaVersion` (and reopen stays there);
  - legacy **unversioned** DB (raw handle creates the schema without setting
    `user_version`, seeds a pinned row with non-empty raw + a plain row) →
    `Open` migrates to v1 and every field (pinned/raw/tags/title/type) reads back
    intact;
  - `user_version = schemaVersion+1` with a sentinel row → `Open` fails with an
    error naming both versions, returns nil, and the file is untouched
    (version + row count unchanged).
- **`README.md`** — one concise paragraph in the data-persistence section:
  older DBs migrate forward automatically; a newer-than-supported DB is rejected
  at startup and left unchanged (upgrade or restore).

## Key decisions / notes

- **Why user_version 0 == legacy == structurally v1.** Every prior release ran
  the same `CREATE IF NOT EXISTS` items schema and never set `user_version`, so
  existing databases read 0 and are already shaped like v1. The 0→1 migration is
  therefore a safe re-apply (idempotent CREATE IF NOT EXISTS) plus a version
  stamp — no column/table changes, no backfill, no data risk.
- **`PRAGMA user_version` needs a literal**, not a bound parameter. The value
  written is always our own trusted integer constant (`v+1`), formatted inline —
  no injection surface.
- **Conservative by design**: no destructive reset/auto-rebuild on mismatch, no
  downgrade path, no new dependencies, no daemon. Future schema changes add a
  `migrateStep` case and bump `schemaVersion`.
- Backup/restore is unchanged; this only adds the open-time version guard.

## Validation

`gofmt -l internal/store` clean; `go vet ./...` clean; `go test ./...` passes
(new tests: TestFreshDBGetsCurrentSchemaVersion,
TestLegacyUnversionedDBMigratesAndPreservesData,
TestNewerThanSupportedVersionFailsWithoutMutation); `go build ./...` OK.
