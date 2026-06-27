# infowall — Agent Entry Guide

> `AGENTS.md` and `CLAUDE.md` must remain identical. Update both files in the
> same change. This guide is the short, always-read entry point; the full
> product/API reference lives in [`README.md`](README.md), and deeper design
> notes belong in `docs/working-logs/YYYY-MM-DD-topic.md`.

---

## Project

infowall is a single-binary personal information wall: a feed of markdown cards
(notes, papers, links, images, stock charts) pushed in from anywhere and
rendered in reverse-chronological order in the browser. New items arrive over a
Server-Sent Events stream so the wall updates live. The server is one Go binary
with the production React/Vite frontend embedded via `//go:embed`, backed by
SQLite. No external services, no accounts.

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

- Production: `./bin/infowall serve --addr :8899 --db infowall.db`
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

If this project is later put under git, prefer a feature branch over committing
directly to the default branch, and use conventional commit messages
(`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).

## Project Map

```text
infowall/
├── cmd/infowall/        CLI entry point + //go:embed of the built frontend
│   ├── main.go          subcommands: serve / push / list / pin / unpin / delete / version
│   ├── dist_prod.go     //go:build !dev — embeds cmd/infowall/dist
│   └── dist_dev.go      //go:build dev  — no embed (serves via Vite proxy)
├── internal/
│   ├── server/          HTTP server: REST API, SSE endpoint, SPA static serving, dev proxy
│   ├── parser/          markdown + YAML frontmatter parser (goldmark) + tests
│   ├── store/           SQLite store (modernc.org/sqlite)
│   ├── feed/            in-process pub/sub hub broadcasting SSE events
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
| `INFOWALL_DB` | `infowall.db` | `serve` | SQLite path (overridden by `--db`) |

When `INFOWALL_API_KEY` / `--api-key` is set, **all writes and the SSE stream**
require either `Authorization: Bearer <key>` or `?key=<key>` (the query form is
for `EventSource`, which cannot set headers). The SQLite DB file and the
`bin/`, `web/dist`, and `cmd/infowall/dist` build artifacts are generated and
git-ignored — do not commit them.

## Pitfalls

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
- **Type ↔ model drift**: a new content type touches four places — the Go model
  constants, the parser, the TS `Item` type, and a registered renderer. Update
  all four together.
- **Frontend lint is type-check only**: `npm run lint` runs `tsc --noEmit`;
  there is no ESLint. Don't assume style auto-fixing — keep code clean manually.

## Working Logs

Significant development work should add a focused log under
`docs/working-logs/YYYY-MM-DD-topic.md` covering: what changed, why, key design
decisions, and any pitfalls discovered. Keep these root agent files short — when
a lesson becomes stable, add a one-line navigation cue here and put the detail
in a working log.
