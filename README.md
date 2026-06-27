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

---

## CLI reference

```
infowall serve    [--addr :8899] [--db infowall.db] [--dev] [--api-key KEY]
infowall push     [file|-] [-t TYPE] [--server URL] [--api-key KEY] [--pin]
infowall list     [--limit N] [--type TYPE] [--json]
infowall pin      <id>
infowall unpin    <id>
infowall delete   <id> [--yes]
infowall version
```

### `serve`

Starts the HTTP + SSE server.

| Flag         | Default              | Purpose                                       |
|--------------|----------------------|-----------------------------------------------|
| `--addr`     | `:8899`              | Listen address                                |
| `--db`       | `infowall.db`        | SQLite database path                          |
| `--dev`      | `false`              | Proxy `/` to the Vite dev server on `:5173`   |
| `--api-key`  | *(none)*             | If set, all writes and SSE require this token |

### `push`

Push a markdown item from a file or stdin. If the first line is `---`, the
document is parsed as YAML frontmatter + markdown body; otherwise, `--type`
wraps the body in a minimal frontmatter block. Use `--pin` to mark the item as
pinned on push.

```bash
./bin/infowall push note.md                          # from a file
echo "done for today" | ./bin/infowall push -        # from stdin (note)
./bin/infowall push - -t link --pin < link.md        # override type, pin it
```

### `list`

Print recent items in a tabular view. Use `--json` for raw JSON. `--type`
filters the feed server-side.

### `pin` / `unpin` / `delete`

Operate on an item by id (full or first-8-hex prefix from `list`). `delete`
prompts for confirmation unless `--yes` is passed.

---

## Content format

Every item is a markdown document. An optional YAML frontmatter block (delimited
by `---`) controls metadata; everything after the closing `---` is the body.

```markdown
---
type: paper
title: Attention Is All You Need
authors: [Vaswani et al.]
published: 2017-06-12
tags: [ml, transformer]
pinned: false
---

The body is full markdown: **bold**, lists, code blocks, tables, etc.
```

If no frontmatter is present the item defaults to `type: note` and the title
is derived from the first `# Heading` or first non-empty line. The `type`
field selects which renderer the frontend uses for the card.

---

## Built-in content types

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
| GET    | `/api/items?limit=50&offset=0&type=paper` | List items (newest first)         |
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
