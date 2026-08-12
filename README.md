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
./bin/infowall serve --addr :8899 --db infowall.db --default-view workbench

# In another terminal, push a note:
echo "# hello world" | ./bin/infowall push -

# Push a file with YAML frontmatter:
./bin/infowall push examples/paper.md
```

Open <http://localhost:8899> in a browser. New items appear live without a
page refresh. On a fresh empty wall, the app shows compact copyable commands
for `infowall health --json` and the first markdown push.

Choose which interface an unqualified browser URL opens with
`--default-view infowall|workbench` (or `INFOWALL_DEFAULT_VIEW`). Explicit
links such as `#wall` and `#workbench/projects` always keep their destination.
The same choice is available from **Default home / 默认首页** in the shared
page header. It is persisted in the server SQLite database and shared by all
browsers; the startup value only initializes databases without the setting.

The personal demand workbench is available at
<http://localhost:8899/#workbench/demands>. SQLite remains the sole source of
truth; the browser and CLI both update it through the local API:

```bash
# Create a project and a demand, then append progress.
./bin/infowall project create --name "InfoWall" --json
./bin/infowall demand create --title "完善个人需求工作台" \
  --status pending --priority p1 --project-hint "InfoWall" --json
./bin/infowall demand progress DEMAND_ID --text "已完成第一轮联调" \
  --link "https://example.test/jobrun/123" --json

# Named resource links can carry type, title, state, and a stable identity.
./bin/infowall demand progress DEMAND_ID --text "Trial 已进入 RUNNING" \
  --links '[{"kind":"trial","external_id":"394541347","title":"MIX 验收 Trial 394541347","url":"https://example.test/trial/394541347","state":"RUNNING","dedupe_key":"trial:394541347"}]' --json

# Create the one-way Feishu mirror, or bind an existing document URL.
./bin/infowall sync feishu setup --create --json
./bin/infowall sync feishu setup --doc "https://example.feishu.cn/docx/TOKEN" --json
./bin/infowall sync feishu status --json
```

Agents should discover the installed contract instead of parsing help text:

```bash
./bin/infowall agent spec --json
printf '%s\n' '{"title":"整理发布验收清单","status":"pending","sources":[{"kind":"agent","dedupe_key":"agent:release-checklist:v1"}]}' \
  | ./bin/infowall demand apply --input - --json
```

`demand apply` accepts one demand, an array, or `{ "demands": [...] }` and is
the preferred retry-safe write path. JSON failures keep stdout empty and add
stable `error_code`, `retryable`, `http_status`, and recovery `hint` fields on
stderr.

The repository also contains the Codex skill at
`skills/infowall-demand`. Install it by linking that directory into
`${CODEX_HOME:-$HOME/.codex}/skills/infowall-demand`; it scans Feishu messages
on demand, retains only demand evidence, and imports candidates through the
running local service.

### Automatic activity ingestion

InfoWall can reconcile Feishu messages and completed local Codex/Claude turns
every 30 minutes from 09:00 through 23:00 in `Asia/Shanghai`. Each source uses
a persisted success watermark with a five-minute overlap; first enablement or
recovery reads at most the latest 12 hours. An empty increment never starts a
model.

Install the local lifecycle hooks once, then enable the built-in scheduler:

```bash
./bin/infowall hooks install --bin "$(pwd)/bin/infowall" --server http://127.0.0.1:8899 --json
./bin/infowall hooks status --json
./bin/infowall scan activity setup --json
./bin/infowall scan activity status --json
./bin/infowall scan activity now --json
./bin/infowall scan activity runs --json
```

`scan feishu` remains a compatibility alias for `scan activity`. The hook
installer merges `UserPromptSubmit` and `Stop` into existing Codex and Claude
settings, never replaces unrelated hooks, and returns within one second. If
the service is unavailable, compact events are written with mode `0600` below
`~/.infowall/spool/conversations` and are deleted after import or 24 hours.
Codex requires the user to trust newly configured hooks from `/hooks`; a
configured but untrusted hook does not run.

Only the user goal, final assistant result, local session/turn identity,
working directory, and direct URLs are retained. Tool logs, reasoning, ANSI
terminal output, and full transcripts are excluded. The summary runner reads
file-backed bounded inputs with only `Read`/`Glob`; local Codex and Claude
events may update existing demands or enter review, but cannot create demands.

For its primary analyzer, InfoWall reads one existing **local** Claude day1
tab from `~/.claude_hub/tabs.json` and launches its own ephemeral restricted
Claude Code process. It does not modify Claude Hub, call a Hub API, install
anything into Hub, or inspect remote-agent transcripts. Only the selected tab
ID, a one-way configuration fingerprint, and health are stored; credentials
remain process-local. If day1 fails twice, InfoWall uses the same temporary
files with Codex and exposes the fallback plus input/cache/output token counts
in the workbench. Pin a specific local tab with `serve --claude-day1-tab ID`
when auto-detection is ambiguous.

The browser groups same-topic items into topic panels by default: links, notes,
papers, images, and charts each get their own fixed-height panel with the
newest items first and an internal scroll area. Use the stack/columns layout
controls and presets to choose vertical topic panels or side-by-side topic
columns. Column order, width, and panel-height changes are stored in local
browser storage and restored on the next visit. Drag a panel's bottom edge or
use its height buttons to resize it; opening a topic header shows that topic's
feed by itself for focused reading.

---

## Formal local operation: go-live checklist

A single pass to confirm a local deployment is ready. Each step links to the
detailed section below — this list only sequences them.

1. **Build the binary**: `make build` (embeds the frontend into `bin/infowall`).
2. **Choose fixed paths/secret**: pin `--db`/`INFOWALL_DB`, `--addr`, and
   `INFOWALL_API_KEY`. See [always-on service](#formal-local-operation-always-on-service).
3. **Start & verify health**: run `serve`, then
   [`infowall health --json`](#formal-local-operation-startup-verification).
4. **Run the diagnostic**: [`infowall doctor --json`](#doctor) confirms health
   plus that the API key reads the protected API.
5. **Confirm the database**: [`infowall db info --json`](#db-info--db-backup)
   shows the active path, item count, and WAL state.
6. **Take a restore point**:
   [`infowall db backup --out <path>`](#db-info--db-backup); know the
   restore-by-copy steps in
   [data persistence & backup](#formal-local-operation-data-persistence--backup).
7. **Optional archive**: [`infowall export --out <path>`](#export) for an
   audit/migration copy (separate from a DB backup).
8. **Open the wall**: load the browser, confirm the live connection indicator
   reads **Live**, and that search/filter and the two-step
   [delete](#pin--unpin--delete) guard behave.
9. **Optional always-on**: install the launchd template only when you want the
   service to survive restarts — see
   [always-on service](#formal-local-operation-always-on-service).

Everything is **local-only** (no cloud, accounts, or remote sync); durability is
your filesystem plus the backups you take.

---

## Formal local operation: startup verification

For day-to-day local operation, start the server and verify it from a second
terminal before pushing data or opening the wall:

```bash
# Terminal 1: start the local service
./bin/infowall serve --addr :8899 --db infowall.db

# Terminal 2: verify the configured endpoint
./bin/infowall health --server http://localhost:8899 --json
```

Expected JSON is intentionally small and safe:

```json
{
  "ok": true,
  "reachable": true,
  "server": "http://localhost:8899",
  "http_status": 200,
  "service": "infowall",
  "service_status": "ok",
  "version": "dev",
  "commit": "none",
  "ts": "2026-07-01T00:00:00Z"
}
```

`infowall health` reads `INFOWALL_URL` unless `--server` is passed, exits
non-zero on network errors or non-2xx responses, and in `--json` mode writes
`{"error": "..."}` to stderr on failure. The underlying `GET /api/health`
endpoint is read-only and does not require an API key, even when the server was
started with `--api-key`.

For one repeatable local diagnostic, use `doctor`. It checks health first, then
performs a read-only authenticated `GET /api/items?limit=1` probe to confirm the
configured API key can access protected API paths. It does not write items,
touch the DB file, or create backups:

```bash
./bin/infowall doctor --server http://localhost:8899 --api-key "$INFOWALL_API_KEY" --json
```

Troubleshooting:

- **Wrong URL**: check `INFOWALL_URL` and any `--server` override. Flags take
  precedence over environment variables.
- **Server down**: start `./bin/infowall serve --addr :8899 --db infowall.db`
  and make sure the listen address matches the health command.
- **API key confusion**: health is intentionally unauthenticated. If health
  passes but `doctor` reports `server 401`, set `INFOWALL_API_KEY` or pass
  `--api-key` to protected API commands.

---

## Formal local operation: always-on service

For formal daily use, keep the same binary, DB path, and API key every time the
machine restarts. Choose those values once, then verify the service after every
start/restart:

```bash
export INFOWALL_URL=http://127.0.0.1:8899
export INFOWALL_DB="$HOME/Library/Application Support/infowall/infowall.db"
export INFOWALL_API_KEY=replace-with-a-long-random-local-secret
mkdir -p "$HOME/Library/Application Support/infowall" "$HOME/Library/Logs/infowall"

./bin/infowall serve --addr 127.0.0.1:8899 --db "$INFOWALL_DB" \
  --api-key "$INFOWALL_API_KEY"

./bin/infowall health --server "$INFOWALL_URL" --json
./bin/infowall doctor --server "$INFOWALL_URL" --api-key "$INFOWALL_API_KEY" --json
./bin/infowall db info --db "$INFOWALL_DB" --json
```

Keep `--addr` on `127.0.0.1:8899` for a private laptop-only wall. Binding to
`0.0.0.0` or a LAN address exposes the wall to other devices; do that only when
intended, and always keep `INFOWALL_API_KEY`/`--api-key` set. Logs are written
to stdout/stderr, so run the command from a terminal during manual checks or
send those streams to files when using a service manager.

On macOS, use [`examples/launchd/com.example.infowall.plist`](examples/launchd/com.example.infowall.plist)
as a template if you want launchd to keep the service running. The template is
not installed by Infowall and contains placeholders only. Copy it to
`~/Library/LaunchAgents/`, replace every placeholder path/secret with local
values, keep the copied file private (`chmod 600`), then lint and load your
copy:

```bash
mkdir -p "$HOME/Library/Application Support/infowall" "$HOME/Library/Logs/infowall"
plutil -lint ~/Library/LaunchAgents/com.example.infowall.plist
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/com.example.infowall.plist
launchctl kickstart -k "gui/$(id -u)/com.example.infowall"

./bin/infowall health --server http://127.0.0.1:8899 --json
./bin/infowall doctor --server http://127.0.0.1:8899 --api-key "$INFOWALL_API_KEY" --json
./bin/infowall db info --db "$HOME/Library/Application Support/infowall/infowall.db" --json
```

Stop and restart the local service with launchd when you need to verify a fresh
start:

```bash
launchctl bootout "gui/$(id -u)/com.example.infowall"
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/com.example.infowall.plist
launchctl kickstart -k "gui/$(id -u)/com.example.infowall"
./bin/infowall health --server http://127.0.0.1:8899 --json
```

Check the log paths from your copied plist when health fails, then use the
startup verification and data persistence sections above/below to confirm the
URL, API key, DB path, and backup/restore state.

---

## Formal local operation: data persistence & backup

Infowall stores everything in a single local SQLite database. For a formal
single-machine run, treat that file as the source of truth and back it up on a
schedule you control. Everything here is intentionally **local-only**: there is
no cloud sync, no remote/offsite backup service, no accounts, and no
multi-device data model. Durability is your filesystem plus the backups you take.

**1. Pin the DB path explicitly.** Always pass `--db` (or set `INFOWALL_DB`) so
the database does not depend on the current working directory:

```bash
./bin/infowall serve --addr :8899 --db /srv/infowall/infowall.db
```

A nested path is handled deliberately: the parent directory is created if it is
missing. If the path is unusable (e.g. it points at a directory, or a parent
component is a regular file) the server fails at startup with an error naming
the offending path — it never silently falls back to a temp or in-memory
database, so you cannot accidentally serve from throwaway storage.

The database carries a schema version (SQLite `PRAGMA user_version`). Databases
from earlier releases open and migrate forward automatically with no data loss.
A database written by a **newer** infowall than the one you are running is
rejected at startup with a clear error (it is left unchanged) — upgrade the
binary or restore a compatible backup rather than risk a downgrade.

**2. Set an API key if the wall is reachable by anyone else.** With
`--api-key`/`INFOWALL_API_KEY` set, all writes and the SSE stream require the
token; only `GET /api/health` stays open. The `db` commands operate on the file
directly and do not need the key.

**3. Inspect the active database.**

```bash
./bin/infowall db info --db /srv/infowall/infowall.db --json
```

Reports the absolute path, file size, WAL/SHM sidecar presence, item count, and
whether the schema is initialized — a quick way for an operator or agent to
confirm which file is live and that it is healthy.

**4. Back up safely, even while serving.**

```bash
./bin/infowall db backup --db /srv/infowall/infowall.db \
  --out /srv/infowall/backups/infowall-$(date +%F-%H%M).db --json
```

`db backup` uses SQLite `VACUUM INTO`, which is consistent against a live,
WAL-mode database. Prefer it over `cp infowall.db backup.db`: a plain copy of a
running WAL database can miss un-checkpointed pages in the `-wal` sidecar and
produce a torn backup. The command refuses to overwrite an existing `--out`
file, so give each backup a unique name (e.g. a timestamp) and rotate/retain
them with your own tooling (cron + `find -mtime`, etc.).

**5. Export portable item archives when you need audit/migration data.**
SQLite backups are the right tool for full restore. `infowall export` is a
read-only API export for portable archives: it writes JSON Lines or JSON with
each item's id, topic/type, title, tags, pinned state, timestamps, body, raw
markdown source, and metadata such as URL/image/chart fields.

```bash
./bin/infowall export --server http://localhost:8899 \
  --api-key "$INFOWALL_API_KEY" --out /srv/infowall/exports/items-$(date +%F).jsonl --json

# selected items or a single topic:
./bin/infowall export <id-1> <id-2> --out selected-items.jsonl --json
./bin/infowall export --topic paper --format json --out paper-items.json --json
```

**6. Restore by copying a backup into place (no destructive CLI command).**
Restore is a deliberate manual step:

```bash
# 1. Stop the server (Ctrl-C / your service manager) so nothing is writing.
# 2. Move the current files aside, including any WAL/SHM sidecars:
mv /srv/infowall/infowall.db     /srv/infowall/infowall.db.old      2>/dev/null || true
mv /srv/infowall/infowall.db-wal /srv/infowall/infowall.db-wal.old  2>/dev/null || true
mv /srv/infowall/infowall.db-shm /srv/infowall/infowall.db-shm.old  2>/dev/null || true
# 3. Copy the chosen backup into the live path:
cp /srv/infowall/backups/infowall-2026-07-01-0900.db /srv/infowall/infowall.db
# 4. Start the server again and verify:
./bin/infowall serve --addr :8899 --db /srv/infowall/infowall.db &
./bin/infowall db info --db /srv/infowall/infowall.db --json
```

A `VACUUM INTO` backup is a fully self-contained database with no sidecar files,
so copying just the single backup file is sufficient. There is intentionally no
`db restore` command — overwriting the live database is high-risk, so it is left
as an explicit, reviewable manual step.

---

## CLI reference

```
infowall serve    [--addr :8899] [--db infowall.db] [--dev] [--api-key KEY]
infowall push     [file|- ...] [-t/--topic TOPIC] [--type TYPE] [--pin] [--server URL] [--api-key KEY] [--json]
infowall list     [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
infowall get      <id> [--raw] [--server URL] [--api-key KEY] [--json]
infowall export   [id ...] [--format jsonl|json] [--out PATH] [--limit N] [--topic TOPIC] [--type TYPE] [--server URL] [--api-key KEY] [--json]
infowall pin      <id> [--server URL] [--api-key KEY] [--json]
infowall unpin    <id> [--server URL] [--api-key KEY] [--json]
infowall delete   <id> [--yes] [--server URL] [--api-key KEY] [--json]
infowall health   [--server URL] [--api-key KEY] [--json]
infowall doctor   [--server URL] [--api-key KEY] [--json]
infowall db info   [--db infowall.db] [--json]
infowall db backup --out PATH [--db infowall.db] [--json]
infowall version
```

Every command that talks to the server accepts `--server`, `--api-key`, and
`--json`, and honours the `INFOWALL_URL` / `INFOWALL_API_KEY` environment
variables (flags take precedence). Flags may appear before or after positional
arguments. See [Agent / scripting usage](#agent--scripting-usage) for the JSON
output and exit-code contract.

The `db` subcommands are different: they operate **directly on the local SQLite
file** (`--db`, or `INFOWALL_DB`, default `infowall.db`) rather than over HTTP,
so they work whether or not a server is running. They still follow the same
JSON/stderr/exit-code contract.

### `serve`

Starts the HTTP + SSE server.

| Flag         | Default              | Purpose                                       |
|--------------|----------------------|-----------------------------------------------|
| `--addr`     | `:8899`              | Listen address                                |
| `--db`       | `infowall.db`        | SQLite database path                          |
| `--dev`      | `false`              | Proxy `/` to the Vite dev server on `:5173`   |
| `--api-key`  | *(none)*             | If set, all writes and SSE require this token |

### `health`

Checks the configured server's `GET /api/health` endpoint. It uses
`INFOWALL_URL` by default, or `--server` when provided. `--json` prints a
structured success object to stdout and writes a structured error to stderr on
failure.

```bash
./bin/infowall health --json
INFOWALL_URL=http://localhost:8899 ./bin/infowall health --json
```

### `doctor`

Runs a read-only local operation diagnostic using the same `INFOWALL_URL`,
`INFOWALL_API_KEY`, `--server`, `--api-key`, and `--json` conventions as the
other server-talking commands. It first verifies `GET /api/health`, then checks
that the configured API key can read the protected API with
`GET /api/items?limit=1`.

`doctor --json` prints a compact summary with health, auth/read-check status,
and next-step commands for DB info and backup verification. It exits non-zero
with a JSON error on stderr when the server is down/unhealthy or when protected
API paths reject the configured key.

```bash
./bin/infowall doctor --json
./bin/infowall doctor --server http://localhost:8899 --api-key "$INFOWALL_API_KEY" --json
```

### `db info` / `db backup`

Local SQLite persistence commands. They act on the database **file** (`--db`, or
`INFOWALL_DB`, default `infowall.db`), not a running server.

`db info` reports the resolved (absolute) DB path, whether the file exists, its
size, whether the `-wal` / `-shm` sidecar files are present, the item count, and
whether the schema is initialized. Opening the DB initializes the schema if the
file is new (the same thing `serve` does), so `db info` on a fresh path reports
an initialized, empty database.

`db backup --out PATH` writes a consistent snapshot using SQLite `VACUUM INTO`.
This is safe to run **while the server is live** — it takes a read transaction
and writes a fully-checkpointed, defragmented copy, so it does not miss
un-checkpointed WAL pages the way a raw `cp` of the `.db` file can. It refuses to
overwrite an existing `--out` file and creates the destination's parent
directory if needed.

```bash
./bin/infowall db info --json
./bin/infowall db info --db /srv/infowall/infowall.db --json
./bin/infowall db backup --out backups/wall-$(date +%F).db --json
```

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
(the full item objects plus pagination metadata). `--topic` filters the feed
server-side; `--type` is a compatibility alias.

### `get`

Fetch a single item by id. Prints a readable summary plus the rendered body, or
the full item JSON with `--json`. Pass `--raw` to include the original markdown
source (the rendered/`--json` body otherwise omits it). A missing id exits
non-zero.

### `export`

Read-only portable item archive from the running service. With no positional
ids, `export` pages through `GET /api/items` and then fetches each item with
`GET /api/items/{id}?raw=1`; with positional ids, it exports exactly those ids
in the order given. It never writes to the SQLite database or backup files.

Default output is JSON Lines on stdout, one full item per line. Use
`--out PATH` to write the archive to a file; existing files are refused so an
operator does not accidentally overwrite an archive. `--format json` writes one
JSON object with `format`, `count`, and `items`. `--json` keeps JSON errors on
stderr and, when used without `--out`, defaults to the JSON object format; when
used with `--out`, stdout is a compact summary of the file written.

```bash
./bin/infowall export --out archive.jsonl --json
./bin/infowall export --topic paper --format json --out papers.json --json
./bin/infowall export <id-1> <id-2> --out selected.jsonl --json
```

Use `export` for audit, migration, or external archival workflows. Use
`db backup` for a complete SQLite restore point.

### `pin` / `unpin` / `delete`

Operate on an item by its id (as shown by `list`). `delete` prompts for
confirmation unless `--yes` is passed; in `--json` mode it never prompts (so it
is safe for automation) and emits `{"id": "...", "deleted": true}`.

### Agent / scripting usage

The CLI is designed to be driven by scripts and agents:

- **`--json` everywhere.** `push` prints an array of per-source result objects
  (`source`, `id`, `type`, `title`, or `error`); `type` is the JSON field that
  stores the item's topic for API compatibility. `list` / `get` / `pin` /
  `unpin` echo the server's JSON; `export` prints JSON Lines or a JSON export
  object; `delete` prints `{"id", "deleted"}`; `health` and `doctor` print
  compact diagnostic objects.
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

# archive all current items without mutating the service
infowall export --out archive.jsonl --json
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
| GET    | `/api/health`                     | Safe unauthenticated health JSON         |
| GET    | `/api/items?limit=50&offset=0&topic=paper` | List items (newest first); `type=paper` also works; `q=` full-text search, `pinned=1` pinned-only |
| GET    | `/api/items/{id}?raw=1`           | Fetch one item (`raw=1` includes source) |
| POST   | `/api/items`                      | Create an item                           |
| POST   | `/api/items/{id}/pin?pinned=1`    | Pin/unpin an item                        |
| DELETE | `/api/items/{id}`                 | Delete an item                           |
| GET    | `/events`                         | Server-Sent Events stream                |

`GET /api/health` is intentionally unauthenticated and safe for local startup
checks. When `INFOWALL_API_KEY` / `--api-key` is set, other API requests and
the SSE stream must carry either `Authorization: Bearer <key>` or `?key=<key>`
as a query parameter (for EventSource, which cannot set headers).

`GET /api/items` is bounded: `limit` is clamped to `1..200` and defaults to 50.
Offset pagination remains supported for existing clients. Newer clients can use
the opaque `cursor` returned as `next_cursor` for stable incremental history
loading:

```json
{
  "items": [{ "id": "...", "type": "note", "title": "..." }],
  "has_more": true,
  "next_cursor": "opaque"
}
```

Search and filtering are applied server-side over the **full history**, not just
the loaded page. Optional query params compose with each other and with
`cursor`/`offset` pagination and topic panels:

- `q=<text>` — case-insensitive full-text search. Each whitespace-separated
  term must match somewhere in the title, body, type, tags, or meta (terms are
  AND-ed); `%`/`_` in a term are matched literally.
- `type=` / `topic=` — restrict to one content type.
- `pinned=1` — pinned items only.

Omitting these params returns the same results as before (fully
backward-compatible). The browser search/filter controls drive these params, so
typing a query searches the entire local database; **Load more** pages the
active filtered query.

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
| `INFOWALL_DB`      | `infowall.db`            | `serve`, `db` | SQLite path (overridden by --db)   |
| `INFOWALL_DEFAULT_VIEW` | `workbench`         | `serve`    | Default interface: `infowall` or `workbench` |

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
