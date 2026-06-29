# infowall

Infowall is a single-binary personal information wall: a feed of markdown cards
(notes, papers, links, images, charts) you push to from anywhere, rendered in
reverse-chronological order in a browser. Think of it as a private dashboard
where each item knows how to display itself — papers show authors and an
abstract, links render as cards with a favicon and description, stock symbols
render live candlestick charts inline — and new items arrive over a
Server-Sent Events stream so the wall updates in real time.

The server is a Go binary with an embedded React/Vite frontend (production
build is a single ~12MB file) backed by SQLite. No external dependencies, no
accounts, no cloud. Run it on a laptop or a cheap VPS, `curl` or `infowall
push` items into it, and pin the tab.

---

## Quick start

```bash
# Build (requires Go 1.22+ and Node 20+ for the frontend build):
make build

# Start the server:
./bin/infowall serve --addr :8899 --db infowall.db

# In another terminal, push a note:
echo "# hello world" | ./bin/infowall push -

# Push a file with YAML frontmatter:
./bin/infowall push examples/paper.md
```

Open <http://localhost:8899> in a browser. New items appear live without a
page refresh.

The browser groups same-topic items into topic panels by default: links, notes,
papers, images, and charts each get their own fixed-height panel with the
newest items first and an internal scroll area. Use the stack/columns layout
controls and presets to choose vertical topic panels or side-by-side topic
columns. Column order, width, and panel-height changes are stored in local
browser storage and restored on the next visit. Drag a panel's bottom edge or
use its height buttons to resize it; opening a topic header shows that topic's
feed by itself for focused reading.

---

## CLI reference

```
infowall serve    [--addr :8899] [--db infowall.db] [--dev] [--api-key KEY]
infowall push     [file|- ...] [-t/--topic TOPIC] [--type TYPE] [--pin] [--server URL] [--api-key KEY] [--json]
infowall list     [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
infowall get      <id> [--raw] [--server URL] [--api-key KEY] [--json]
infowall pin      <id> [--server URL] [--api-key KEY] [--json]
infowall unpin    <id> [--server URL] [--api-key KEY] [--json]
infowall delete   <id> [--yes] [--server URL] [--api-key KEY] [--json]
infowall version
```

Every command that talks to the server accepts `--server`, `--api-key`, and
`--json`, and honours the `INFOWALL_URL` / `INFOWALL_API_KEY` environment
variables (flags take precedence). Flags may appear before or after positional
arguments. See [Agent / scripting usage](#agent--scripting-usage) for the JSON
output and exit-code contract.

### `serve`

Starts the HTTP + SSE server.

| Flag         | Default              | Purpose                                       |
|--------------|----------------------|-----------------------------------------------|
| `--addr`     | `:8899`              | Listen address                                |
| `--db`       | `infowall.db`        | SQLite database path                          |
| `--dev`      | `false`              | Proxy `/` to the Vite dev server on `:5173`   |
| `--api-key`  | *(none)*             | If set, all writes and SSE require this token |

### `push`

Upload one or more markdown items. Each argument may be a **file** or `-` for
**stdin**. With no argument it reads stdin. Every document becomes a separate
item. To push a whole folder, expand it with a shell glob (e.g.
`infowall push notes/*.md`) — directory arguments are rejected.

If a document begins with `---` it is parsed as YAML frontmatter + markdown
body; otherwise `--topic` / `--pin` wrap it in a minimal frontmatter block (when
frontmatter is already present, the fields are merged in without overriding an
existing `type` or `topic`). `--type` is kept as a compatibility alias for
older scripts. This is the recommended way to push complex content: keep it in
markdown files rather than squeezing it onto the command line.

```bash
./bin/infowall push note.md                          # one file
./bin/infowall push notes/*.md                        # a folder, via a shell glob
./bin/infowall push a.md b.md c.md                    # several files at once
echo "done for today" | ./bin/infowall push -        # from stdin (note)
./bin/infowall push - -t link --pin < link.md        # set topic, pin it
./bin/infowall push docs/*.md --json                  # machine-readable result
```

`push` is non-interactive whenever it is given file arguments, so it never
blocks. In a batch, a bad source (missing file, a directory, server error) is
reported but does not stop the remaining sources; the command exits non-zero
if **any** source failed.

### `list`

Print recent items in a tabular view. Use `--json` for the raw server JSON
(the full item objects). `--topic` filters the feed server-side; `--type` is a
compatibility alias.

### `get`

Fetch a single item by id. Prints a readable summary plus the rendered body, or
the full item JSON with `--json`. Pass `--raw` to include the original markdown
source (the rendered/`--json` body otherwise omits it). A missing id exits
non-zero.

### `pin` / `unpin` / `delete`

Operate on an item by its id (as shown by `list`). `delete` prompts for
confirmation unless `--yes` is passed; in `--json` mode it never prompts (so it
is safe for automation) and emits `{"id": "...", "deleted": true}`.

### Agent / scripting usage

The CLI is designed to be driven by scripts and agents:

- **`--json` everywhere.** `push` prints an array of per-source result objects
  (`source`, `id`, `type`, `title`, or `error`); `type` is the JSON field that
  stores the item's topic for API compatibility. `list` / `get` / `pin` /
  `unpin` echo the server's JSON; `delete` prints `{"id", "deleted"}`.
- **Structured errors.** In `--json` mode a failure writes
  `{"error": "..."}` to **stderr** and leaves stdout clean.
- **Exit codes.** Any failure (network error, non-2xx response, missing id,
  one-or-more failed sources in a batch) exits non-zero; success exits `0`.
- **No surprise prompts.** `push` with arguments and `delete --json` never read
  from the terminal.

```bash
# pipe complex content from files and collect the new ids
ids=$(infowall push docs/*.md --json | jq -r '.[].id')

# fetch an item's raw markdown back out
infowall get "$id" --raw --json | jq -r '.raw'
```

---

## Content format

Every item is a markdown document. An optional YAML frontmatter block (delimited
by `---`) controls metadata; everything after the closing `---` is the body.

```markdown
---
topic: paper
title: Attention Is All You Need
authors: [Vaswani et al.]
published: 2017-06-12
tags: [ml, transformer]
pinned: false
---

The body is full markdown: **bold**, lists, code blocks, tables, etc.
```

If no frontmatter is present the item defaults to `topic: note` and the title
is derived from the first `# Heading` or first non-empty line. The `topic`
field selects which renderer and topic panel the frontend uses for the card.
`type` remains accepted as a backward-compatible alias in frontmatter and in the
JSON API.

---

## Built-in topics / content types

### `note` (default)

Generic markdown note. No special frontmatter fields; `title` and `tags` are
honored.

```bash
./bin/infowall push examples/note.md
```

### `paper`

Academic paper / article summary.

| Field        | Type           | Description                  |
|--------------|----------------|------------------------------|
| `title`      | string         | Paper title                  |
| `authors`    | string[]       | Author list                  |
| `published`  | date (YYYY-MM-DD) | Publication date         |
| `url`        | string         | Link to paper / arXiv / DOI  |
| `venue`      | string         | Optional venue (e.g. NeurIPS)|
| `tags`       | string[]       | Tags                         |

The markdown body renders as the abstract / notes.

```bash
./bin/infowall push examples/paper.md
```

### `link`

A bookmark / external link rendered as a card.

| Field         | Type   | Description                                |
|---------------|--------|--------------------------------------------|
| `title`       | string | Link title                                 |
| `url`         | string | Target URL                                 |
| `description` | string | One- or two-sentence description           |
| `thumbnail`   | string | Optional OG image URL (1200x630 ideal)     |
| `favicon`     | string | Optional favicon URL                       |
| `tags`        | string[] | Tags                                     |

```bash
./bin/infowall push examples/link.md
```

### `image`

A single image with an optional caption.

| Field     | Type   | Description                       |
|-----------|--------|-----------------------------------|
| `title`   | string | Title                             |
| `url`     | string | Image URL                         |
| `caption` | string | Caption shown under the image     |
| `tags`    | string[] | Tags                             |

```bash
./bin/infowall push examples/image.md
```

### `stock-chart`

A candlestick chart rendered inline with the
[`lightweight-charts`](https://tradingview.github.io/lightweight-charts/)
library.

| Field            | Type     | Description                                   |
|------------------|----------|-----------------------------------------------|
| `symbol`         | string   | Ticker (e.g. `AAPL`, `NVDA`)                  |
| `timeframe`      | string   | Label (e.g. `1d`, `1wk`, `1h`)                |
| `price`          | number   | Last price, shown next to the symbol          |
| `change_percent` | number   | Percent change (green/red colored)            |
| `data`           | object[] | Array of OHLCV candles, see below             |
| `tags`           | string[] | Tags                                          |

Each candle in `data` is `{ time: "YYYY-MM-DD", open, high, low, close, volume? }`.
If any candle has a `volume` field, a volume histogram is rendered below the
price pane.

```bash
./bin/infowall push examples/stock.md
```

---

## HTTP API

All POST bodies accept `text/markdown` (raw markdown bytes), `text/plain`, or
`application/json` with shape `{"raw": "..."}`. Responses are JSON.

| Method | Path                              | Description                              |
|--------|-----------------------------------|------------------------------------------|
| GET    | `/api/health`                     | `{"ok": true, "ts": "..."}`              |
| GET    | `/api/items?limit=50&offset=0&topic=paper` | List items (newest first); `type=paper` also works |
| GET    | `/api/items/{id}?raw=1`           | Fetch one item (`raw=1` includes source) |
| POST   | `/api/items`                      | Create an item                           |
| POST   | `/api/items/{id}/pin?pinned=1`    | Pin/unpin an item                        |
| DELETE | `/api/items/{id}`                 | Delete an item                           |
| GET    | `/events`                         | Server-Sent Events stream                |

When `INFOWALL_API_KEY` / `--api-key` is set, requests must carry either
`Authorization: Bearer <key>` or `?key=<key>` as a query parameter (for
EventSource, which cannot set headers).

---

## Server-Sent Events

Connect to `/events` for a real-time feed. The server sends an immediate
`retry: 3000` directive and a `: ping` comment (so intermediaries see traffic
right away), then pings every 25 seconds to keep the connection alive. Event
names and payloads:

```
event: item.new
data: {"id":"...","type":"note","title":"...","body":"...",...}

event: item.pin
data: {"id":"...","pinned":true}

event: item.delete
data: {"id":"..."}
```

On connect the server also replays the current feed so new subscribers see
existing items without a separate round-trip.

Minimal subscriber:

```bash
curl -sN http://localhost:8899/events
```

---

## Environment variables

| Variable           | Default                  | Used by    | Purpose                             |
|--------------------|--------------------------|------------|-------------------------------------|
| `INFOWALL_URL`     | `http://localhost:8899`  | CLI        | Server base URL for push/list      |
| `INFOWALL_API_KEY` | *(unset)*                | CLI/server | Shared secret for API auth          |
| `INFOWALL_ADDR`    | `:8899`                  | `serve`    | Listen address (overridden by --addr)|
| `INFOWALL_DB`      | `infowall.db`            | `serve`    | SQLite path (overridden by --db)   |

CLI flags take precedence over environment variables.

---

## Development

Two terminals:

```bash
# Terminal 1: Vite dev server with HMR
make web-dev          # cd web && npm run dev

# Terminal 2: Go server in dev mode (proxies / to :5173)
go run -tags dev ./cmd/infowall serve --dev
```

`-tags dev` compiles out the `//go:embed` directive so the server serves the
frontend through the Vite proxy instead of the embedded production build.
The API and SSE endpoints are identical between dev and production.

Run tests with `make test`. The Makefile also has:

```
make web       # build the production frontend into web/dist
make server    # build the production Go binary (copies web/dist → cmd/infowall/dist first)
make build     # alias for make server
make clean     # remove bin/, web/dist, cmd/infowall/dist
```

---

## Layout

```
cmd/infowall/        CLI entry point + embed directives
internal/server/     HTTP server, REST API, SSE hub, SPA static file serving
internal/parser/     Markdown + YAML frontmatter parser (goldmark)
internal/store/      SQLite store (mattn/go-sqlite3)
internal/feed/       In-process pub/sub hub for SSE broadcasts
internal/model/      Item type and shared types
web/                 Vite + React + TypeScript frontend
web/src/components/renderers/  One component per content type
examples/            Sample markdown documents you can push
```

## License

MIT.
