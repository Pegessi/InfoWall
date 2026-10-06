# infowall — Agent Entry Guide

> `AGENTS.md` and `CLAUDE.md` must remain identical. Update both files in the
> same change. This guide is the short, always-read entry point; the full
> product/API reference lives in [`README.md`](README.md), and deeper design
> notes belong in `docs/working-logs/YYYY-MM-DD-topic.md`.

---

**⚠️ RULE #1 — DO NOT DEVELOP DIRECTLY ON `main`. ⚠️**

Every feature, bug fix, UI change, test, doc update, and working-log addition
**must** use an isolated worktree on a feature branch. No exceptions. This is
the single most important rule in this document. See
[Mandatory Workflow](#mandatory-workflow). This convention is aligned with the
`claude_hub` project so agents moving between the two projects behave
consistently.

---

## Project

infowall is a single-binary personal information wall: a feed of markdown cards
(notes, papers, links, images, stock charts) pushed in from anywhere and
rendered in reverse-chronological order in the browser. New items arrive over a
Server-Sent Events stream so the wall updates live. The server is one Go binary
with the production React/Vite frontend embedded via `//go:embed`, backed by
SQLite. The core requires no external service or account. Optional integrations
implement vendor-neutral in-process `connector`, `enricher`, `analyzer`, or
`exporter` contracts; a zero-integration server is valid. Built-in Feishu,
Codebase, Claude, and Codex paths are compatibility adapters, not core
dependencies. This phase does not provide an external HTTP plugin protocol,
dynamic loading, or hot reload.

For end-user docs (CLI reference, content formats, full HTTP API, SSE event
shapes) read [`README.md`](README.md). This guide is for people and agents
*changing* the code.

## Stack

- **Backend / CLI**: Go 1.22+ (single module, standard `net/http`, `flag`).
- **Frontend**: React 18 + TypeScript + Vite 6, Tailwind CSS v4, rendered
  markdown via `react-markdown` + `remark-gfm` + `rehype-highlight`.
- **Storage**: SQLite via pure-Go `modernc.org/sqlite` (no cgo).
- **Markdown/frontmatter**: `goldmark` + `goldmark-meta` (YAML).
- **Charts**: `lightweight-charts` (stock-chart renderer).
- **Package managers**: Go modules for backend, npm for `web/`.

The production binary embeds the built frontend (`cmd/infowall/dist`), so a
release is a single ~12MB file with no runtime dependencies.

## Mandatory Workflow

**This workflow is mandatory. Do not skip steps. Do not take shortcuts.**

For all feature work, bug fixes, UI changes, tests, documentation changes, and
working-log additions — even small ones:

1. Start from clean `main`: `git status` in the main worktree must be clean
   (no modified / untracked source files). If you discover in-flight work on
   `main`, do not pile on top of it — stop, ask the user how to proceed
   (stash, move to a branch, or commit), then continue on a worktree.
2. Create an isolated worktree and branch:
   `git worktree add ../infowall-<slug> -b feat/<slug> main`
   (use `fix/<slug>` for bug fixes, `docs/<slug>` for doc-only, `chore/<slug>`
   for tooling/build). The worktree must live **outside** the main repo
   directory (sibling) so the main worktree never has dirty source files.
3. **Work only inside that task worktree.** Never edit source files in the
   `main` worktree directly. Running `go test ./...` / `npm run lint` inside
   the worktree is fine — build artifacts (`bin/`, `web/dist`,
   `cmd/infowall/dist`) are git-ignored and may appear in the worktree.
4. For frontend changes, run the Vite dev server from that worktree on its
   own port (or the default :5173 if no conflict); stop it before merging.
5. Commit changes with conventional commits (`feat:`, `fix:`, `docs:`,
   `refactor:`, `test:`, `chore:`). Group related changes; one feature = one
   branch (multiple commits within a branch are fine).
6. Run validation appropriate to the touched files (see Development Workflow
   below — at minimum `go vet ./...` + `go test ./...` for Go changes,
   `npm run lint` + `npm run build` for frontend changes, and a smoke test
   of the rebuilt binary for behavior changes).
7. Add a working log under `docs/working-logs/YYYY-MM-DD-<slug>.md` for
   non-trivial changes (see Working Logs). Update `README.md` when
   user-facing behavior changes.
8. **Merge into `main` only after validation and explicit user approval.**
   Never merge to `main` autonomously. After merge, pull the main worktree
   and delete the worktree (`git worktree remove -f ../infowall-<slug>`).

A user request to "merge" / "commit to main" / "push" means complete this
branch-to-main flow. It is not permission to skip the worktree branch.

If you catch yourself writing code on `main`, stop immediately. Stash the
changes, create a worktree, apply the stash there (`git stash pop` inside the
worktree), and continue. Do not commit directly on `main` unless explicitly
instructed to do so in a throwaway/experimental context.

## Commands

Build & release (from repo root, via the `Makefile`):

- `make build` / `make server` — build the production binary into `bin/infowall`
  (builds the frontend, copies `web/dist` → `cmd/infowall/dist`, then
  `go build` with `//go:embed`).
- `make web` — build only the production frontend into `web/dist`.
- `make server-dev` — build the binary with `-tags dev` (no embedded frontend).
- `make test` — `go test ./...` plus the frontend test script if present.
- `make clean` — remove `bin/`, `web/dist`, and `cmd/infowall/dist`.

Frontend (from `web/`):

- `npm run dev` — Vite dev server with HMR on `:5173` (proxies `/api` and
  `/events` to `:8899`).
- `npm run build` — `tsc -b && vite build`.
- `npm run lint` — type-check only (`tsc --noEmit`). **This is the lint gate.**

Run the server:

- Local production: `./bin/infowall serve --addr 127.0.0.1:8899 --db
  "$HOME/Library/Application Support/infowall/personal-workbench.db"`.
- Remote primary: run the same binary with one explicit persistent database,
  publish it through a controlled HTTPS endpoint, and require an API key. Keep
  endpoint, host, port, database, and secret values in local configuration; do
  not commit them.
- Dev (two terminals): `make web-dev` in one, and
  `go run -tags dev ./cmd/infowall serve --dev` in the other. `--dev` proxies
  `/` to the Vite dev server instead of serving the embedded build.

## Development Workflow

infowall is a small project; the workflow is intentionally lightweight.

1. **Understand before editing.** Read this guide and the relevant section of
   `README.md`. Use the [Common Edit Areas](#common-edit-areas) table to find
   the right files instead of grepping blindly.
2. **Backend changes**: edit under `internal/` or `cmd/`, then
   `go build ./...` and `go test ./...`. Keep `gofmt` clean.
3. **Frontend changes**: run `npm run dev` from `web/` against a running dev
   server (`go run -tags dev ./cmd/infowall serve --dev`) so you see HMR.
   Before finishing, run `npm run lint` (type-check) and `npm run build`.
4. **End-to-end check**: build the real binary with `make build` and exercise
   the change through `./bin/infowall push` / the browser when behavior (not
   just internals) changed.
5. **Validate** with the commands appropriate to the files you touched (see
   above) and report what you ran.
6. **Document** meaningful changes: update `README.md` when user-facing
   behavior (CLI, API, content types, env vars) changes, and add a working log
   under `docs/working-logs/` for non-trivial design work.

Use conventional commit messages (`feat:`, `fix:`, `docs:`, `refactor:`,
`test:`, `chore:`). All work must follow the worktree/branch rule above — see
[Mandatory Workflow](#mandatory-workflow).

## Project Map

```text
infowall/
├── cmd/infowall/        CLI entry point + //go:embed of the built frontend
│   ├── main.go          subcommands: serve / push / list / get / pin / unpin / delete / version
│   ├── dist_prod.go     //go:build !dev — embeds cmd/infowall/dist
│   └── dist_dev.go      //go:build dev  — no embed (serves via Vite proxy)
├── internal/
│   ├── server/          HTTP server: REST API, SSE endpoint, SPA static serving, dev proxy
│   ├── parser/          markdown + YAML frontmatter parser (goldmark) + tests
│   ├── store/           SQLite store (modernc.org/sqlite)
│   ├── feed/            in-process pub/sub hub broadcasting SSE events
│   ├── integration/     vendor-neutral in-process extension contracts
│   └── model/           Item type + known content-type constants
├── web/                 Vite + React + TypeScript frontend
│   └── src/
│       ├── components/renderers/  one component per content type + registry.ts
│       ├── components/feed/       feed list, item card, header, icon map
│       ├── components/layout/     page header
│       ├── components/markdown/   shared markdown renderer
│       ├── hooks/                 useFeed (SSE subscription), useTheme
│       └── lib/                   api client, types, time + class-name utils
├── examples/            sample markdown documents you can push
└── Makefile             build / dev / test / clean targets
```

## Common Edit Areas

| Task | Key files |
| --- | --- |
| Add/modify a CLI subcommand or flag | `cmd/infowall/main.go` |
| Change REST API, SSE, or auth | `internal/server/server.go`, `README.md` (API table) |
| Change how markdown/frontmatter is parsed | `internal/parser/parser.go`, `internal/parser/parser_test.go` |
| Change persistence / schema | `internal/store/store.go` |
| Change SSE broadcast / subscriber fan-out | `internal/feed/hub.go` |
| Change integration contracts / composition | `internal/integration/`; keep vendor adapters outside the core package |
| Add a field to items / a new content type | `internal/model/item.go`, parser, `web/src/lib/types.ts`, a renderer + `web/src/components/renderers/registry.ts` |
| Add/modify a frontend card renderer | `web/src/components/renderers/*.tsx`, `registry.ts`, `web/src/components/feed/iconMap.ts` |
| Change the SSE client / feed state | `web/src/hooks/useFeed.ts`, `web/src/lib/api.ts` |
| Change build/embed pipeline | `Makefile`, `cmd/infowall/dist_prod.go`, `web/vite.config.ts` |

## Coding Conventions

**Go**

- Format with `gofmt` (tabs); keep imports grouped stdlib-then-module. Run
  `go vet ./...` before finishing larger changes.
- Every package starts with a `// Package <name> ...` doc comment (see
  `internal/model/item.go`, `internal/server/server.go`). Exported types and
  functions get doc comments.
- Wrap errors with context using `fmt.Errorf("...: %w", err)`; return errors up
  rather than calling `log.Fatal` outside `main`. `cmd/infowall/main.go`
  centralizes exit handling in `run()` → `main()`.
- Keep `internal/` packages dependency-light and free of frontend concerns.
- Content-type strings live as constants in `internal/model/item.go` — reuse
  them, don't hardcode `"note"` etc.

**Frontend (TypeScript / React)**

- TypeScript runs in `strict` mode (`web/tsconfig.app.json`); `npm run lint`
  (i.e. `tsc --noEmit`) must pass with no errors.
- Import from `@/...` (alias to `web/src`, configured in both `vite.config.ts`
  and `tsconfig.app.json`).
- One renderer component per content type under
  `web/src/components/renderers/`, registered through `register(type, Component)`
  in `registry.ts`. `getRenderer()` falls back to `NoteRenderer` for unknown
  types — add new types via the registry, never a `switch`.
- Keep API shapes in `web/src/lib/types.ts` aligned with `internal/model/item.go`.
- Use the shared `cn()` / class utilities in `web/src/lib/utils.ts` rather than
  ad-hoc class concatenation.

## Runtime & Environment

Toolchain required to build from source:

- **Go 1.22+** (matches `go.mod`). `modernc.org/sqlite` is pure Go, so **no cgo
  / C toolchain** is needed.
- **Node 20+** with npm, only for building the frontend (`make web` / the
  `web/` scripts). The shipped binary needs neither Node nor Go at runtime.

Runtime configuration is via flags or environment variables; **flags take
precedence over env vars**:

| Variable | Default | Used by | Purpose |
| --- | --- | --- | --- |
| `INFOWALL_URL` | `http://localhost:8899` | CLI | Server base URL for `push`/`list` |
| `INFOWALL_API_KEY` | *(unset)* | CLI & server | Shared secret for API auth |
| `INFOWALL_ADDR` | `:8899` | `serve` | Listen address (overridden by `--addr`) |
| `INFOWALL_DB` | `infowall.db` (CLI fallback only) | `serve` / `db info` / `db backup` | SQLite path (overridden by `--db`); production deployments must set an explicit persistent path outside the worktree. |

When `INFOWALL_API_KEY` / `--api-key` is set, **all writes and the SSE stream**
require either `Authorization: Bearer <key>` or `?key=<key>` (the query form is
for `EventSource`, which cannot set headers). The SQLite DB file and the
`bin/`, `web/dist`, and `cmd/infowall/dist` build artifacts are generated and
git-ignored — do not commit them.

## Persistent DB & Running a Primary

Every installation must designate exactly one writable primary. It may run
locally or on a remote host, but endpoint, host, port, database, and secret
values are installation-specific configuration and must not be committed. The
built-in `infowall.db` value is a current-directory fallback only; use an
explicit path outside the worktree for persistent serving.

- **Verify a remote primary before writing.** Point `INFOWALL_URL` at the
  operator-provided HTTPS endpoint and provide `INFOWALL_API_KEY`. Confirm
  `infowall health --json` and `infowall doctor --json` before changing data.
- **Keep deployment values local.** Put service-manager settings, reverse-proxy
  configuration, database paths, and secrets in private local configuration.
- **Do not start a retained recovery copy as another writer.** A copied SQLite
  file does not synchronize with the selected primary. Freeze writes and
  reconcile data before an intentional migration or recovery cutover.
- **Before a local start, check nothing is already on :8899.** Run
  `lsof -iTCP:8899 -sTCP:LISTEN`. If a server is already running, do not
  start a second one — either reuse it or stop it explicitly (`kill <pid>`)
  before starting a fresh binary. Two servers on the same port will race;
  whichever bound first keeps the port and the second fails (or silently
  binds a different address).
- **Never use `/tmp/infowall-*.db` for the persistent wall.** `/tmp/` paths
  are for throwaway smoke tests and scratch servers on any free non-production
  port; kill them when done. Anything under `/tmp/` can be deleted by the OS or
  by other agents.
- **Do not overwrite the selected primary database.** When rebuilding the
  binary, deploy it separately from the database and restart on the same
  explicit `--db` path. The only ways to wipe the wall are:
  (a) user explicitly asks for it, (b) running `rm` on the database yourself
  (do not), or (c) running a smoke-test server pointed at a throwaway
  `/tmp/...db` (fine).
- **Do not point `serve --db` at some random path** (e.g. a session-scoped
  `/tmp/infowall-<sessionid>.db` left over from a previous agent) unless
  the user asks for it or you are running an isolated test. Doing so
  silently swaps out the wall and makes the user think their data is gone.
- **CLI `db info` / `db backup` default to the current-directory fallback**
  `infowall.db` unless `--db` is supplied. Always pass the installation's
  explicit persistent path. Backups go to a user-specified path via VACUUM INTO
  and never overwrite an existing file.

For a dev/frontend-only session (HMR, no embedded frontend), use two
terminals as described under [Commands](#commands): `cd web && npm run dev`
in one, `go run -tags dev ./cmd/infowall serve --dev` in the other. **Always
pass an explicit throwaway `--db`** (e.g. `--db /tmp/infowall-dev.db`); never
rely on the current-directory `infowall.db` fallback. Because worktrees live
in sibling directories, that fallback silently creates or opens an *empty*
database next to whichever worktree you are in, which reads as "my workbench
is gone".

## Pitfalls

- **No direct work on `main`**: always create a worktree + feature branch first.
  Even small fixes and doc changes go through a worktree. See
  [Mandatory Workflow](#mandatory-workflow). This is RULE #1.
- **Don't clobber or fork the persistent wall.** Keep one writable primary on
  its explicit persistent database. Do not start a copied database as another
  writer, point production at a throwaway `/tmp/...db`, or remove a database
  unless the user explicitly asks to wipe data. Read
  [Persistent DB & Running a Primary](#persistent-db--running-a-primary).
- **Dev vs. embedded frontend**: production builds embed `cmd/infowall/dist`
  through `dist_prod.go` (`//go:build !dev`). Building or running with
  `-tags dev` switches to `dist_dev.go`, which serves nothing — you **must**
  pass `serve --dev` and run the Vite dev server, or the page will 404.
- **Stale embedded frontend**: `make server` copies `web/dist` →
  `cmd/infowall/dist` before `go build`. Editing `web/` and rebuilding only the
  Go binary will ship the *old* frontend. Use `make build`, not a bare
  `go build`.
- **API-key gating includes SSE**: if you add a write endpoint or a new event
  stream, route it through the same auth check; missing the SSE path is an easy
  miss.
- **Integration boundaries are in-process in this phase**: keep core business
  state independent of vendor SDKs and implement external systems through the
  neutral roles. Do not document or infer HTTP plugins, hot loading, or dynamic
  discovery until those lifecycle and security contracts exist.
- **Type ↔ model drift**: a new content type touches four places — the Go model
  constants, the parser, the TS `Item` type, and a registered renderer. Update
  all four together.
- **Frontend lint is type-check only**: `npm run lint` runs `tsc --noEmit`;
  there is no ESLint. Don't assume style auto-fixing — keep code clean manually.
- **CLI is agent-friendly by contract**: every server-talking subcommand
  supports `--json` (results on stdout, `{"error":...}` on stderr) and exits
  non-zero on any failure; `push` accepts one or more files and/or `-`/stdin
  (directory arguments are rejected — expand folders with a shell glob) and
  never prompts when given arguments. Keep new subcommands consistent —
  register shared flags via `addClientFlags` and route output through the
  `clientConfig` helpers in `cmd/infowall/main.go`.

## Working Logs

Significant development work should add a focused log under
`docs/working-logs/YYYY-MM-DD-topic.md` covering: what changed, why, key design
decisions, and any pitfalls discovered. Keep these root agent files short — when
a lesson becomes stable, add a one-line navigation cue here and put the detail
in a working log.
