# Record the real canonical database path

## Why

Both root agent guides named a canonical database that is not the one the
workbench actually serves from, and the two guides disagreed with each other:

| Path | Claimed by | Reality |
| --- | --- | --- |
| `<repo>/infowall.db` | `CLAUDE.md` / `AGENTS.md` | exists, 196KB, **zero tables** |
| `~/Library/Application Support/infowall/infowall.db` | `README.md` | **does not exist** |
| `~/Library/Application Support/infowall/personal-workbench.db` | nobody | live: 14 tables, `items=4`, `demands=650` |

The live server had been started with an explicit `--db` pointing at the
Application Support path (`cmd/infowall/main.go:183` defaults to the bare
relative `infowall.db`, so no code path produces that location). An agent
following the guides would therefore run `serve` with no `--db`, open the empty
repo-root file, and report an empty wall and an empty workbench.

## Change

- `CLAUDE.md` + `AGENTS.md` (kept byte-identical): the canonical-DB section, the
  `INFOWALL_DB` row of the environment table, the run-server command, the
  overwrite/backup pitfalls, and the dev-session guidance.
- `README.md`: quickstart, day-to-day local operation, formal always-on
  operation, durability notes, `db info` / `db backup`, and the env-var table.
- The documented production command now carries the explicit `--db` path.
  `--addr` is `127.0.0.1:8899`, matching the running process and the README's
  own advice to keep a private wall off the LAN.
- Dev sessions must pass an explicit throwaway `--db`. The previous wording
  ("the dev server still writes to `infowall.db` by default") pointed at the
  current-directory fallback, which inside a worktree opens a fresh empty
  database in a sibling directory.

The `--default-view workbench` and `--claude-0821-preset 0821` flags present on
the running process are omitted from the documented command because both equal
their built-in defaults (`INFOWALL_DEFAULT_VIEW`, `INFOWALL_CLAUDE_0821_PRESET`).

## Decision: the database stays outside the repo

Keeping the file in the Application Support directory was chosen over moving it
into the repo, despite the repo path being the historically documented one:

- `CLAUDE.md` RULE #1 requires every change to happen in a sibling worktree, and
  a worktree is a full checkout. A repo-relative `infowall.db` default means
  every worktree resolves `./infowall.db` to its own file, so the exact failure
  the guides warn about ("silently swaps out the wall and makes the user think
  their data is gone") becomes the default rather than an edge case.
- The repo is a disposable development surface; 64.9MB of live work data should
  not live inside it.
- The absolute Application Support path is unreachable from any worktree, which
  is the property that makes the current setup safe.

## Boundaries

Documentation only. No Go, frontend, schema, or data changes, and no database
was moved or rewritten. The empty `<repo>/infowall.db` is left on disk
(untracked, matched by `*.db` in `.gitignore`) and is now described as a
current-directory CLI fallback rather than the canonical database.

## Validation

- `diff -q CLAUDE.md AGENTS.md` — identical after the change.
- Grep for `<repo>/infowall.db` and `0.0.0.0:8899` across all three files —
  no stale references remain.
- Cross-checked every documented path and flag against the running process
  (`ps -eo command`), the Application Support directory listing, and
  `sqlite3` table/row counts on both database files.
