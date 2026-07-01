# 2026-07-01 — Full-history feed search & filter (server-side)

## Goal

After cursor/keyset pagination landed, search/filter only covered the items
already loaded in the browser (the newest page). On a growing local DB that
makes search misleading. This pushes text search + type/topic + pinned filtering
into the SQLite query path so search covers the **entire history**, while
staying on the existing cursor/offset pagination and keeping the API/response
shape backward-compatible.

## What changed

- **`internal/store/store.go`**
  - `ListOptions` gains `Query string` and `PinnedOnly bool`.
  - `ListPage` builds deterministic predicates that compose with the existing
    `TypeFilter`, keyset `After` cursor, and `pinned DESC, created_at DESC, id
    DESC` ordering:
    - Each whitespace-separated search term →
      `lower(title||' '||body||' '||type||' '||tags||' '||meta) LIKE ? ESCAPE '\'`
      (terms AND-ed; tags/meta are the JSON text columns).
    - `PinnedOnly` → `pinned = 1`.
  - Helpers `searchTerms` (split) and `likeEscape` (escape `\ % _` so wildcards
    in a term match literally).
- **`internal/server/server.go`** — `GET /api/items` parses optional `q` and
  `pinned` params into `ListOptions`; all other params and the
  `{items, has_more, next_cursor}` shape are unchanged.
- **`web/src/lib/api.ts`** — `fetchItemsPage` accepts `query` + `pinnedOnly` and
  sends `q` / `pinned=1`.
- **`web/src/hooks/useFeed.ts`** — now takes a `FeedFilter`. It re-queries page 1
  (debounced ~200ms) whenever the filter changes, and `loadMore` pages the
  **active** filtered query via cursor. SSE `item.new` is only prepended when it
  matches the active filter (via `itemMatches`) so the filtered view stays
  coherent; pin/delete SSE handling and the single-stream/connection-status
  behavior are unchanged.
- **`web/src/components/feed/FeedList.tsx`** — lifts filter state and passes it
  into `useFeed`; removed the client-side `filterItems` pass (items are already
  server-filtered). Distinct states: empty wall (no filter, no items → first-run
  `EmptyState`) vs no-results (filter active, zero results → `NoResultsState`).
  Controls stay visible when a filter is active even with zero results so it can
  be cleared. End-of-list copy is query-aware.
- **`web/src/components/feed/FeedFilterBar.tsx`** — count copy corrected for
  full-history search: "N matches" (with `+` when more pages exist) instead of
  the old "N of M loaded".

## Deliberate choices / tradeoffs

- **LIKE, not FTS5.** Deterministic `LIKE` over the existing text columns is
  dependency-light and needs no schema/migration or index rebuild. Good enough
  for a single-machine local wall; FTS5 would be a larger, heavier change.
  Documented so a future task can upgrade if the DB grows large.
- **Search matches meta/tags as JSON text.** A term can match JSON punctuation
  or keys inside `meta` — an acceptable approximation that mirrors the previous
  client-side haystack; noted as a minor false-positive source.
- **SSE live items are filtered client-side against the active query**
  (`itemMatches`) rather than re-issuing a server query per event. A new item
  that doesn't match the current filter simply doesn't appear until the query is
  changed/cleared or the stream is retried; a matching item appears immediately
  at the top (reverse-chron). This keeps the filtered view coherent without a
  server round-trip per event.
- **Backward compatibility:** no new params → identical results; cursor/offset,
  `type`/`topic`, `raw`, and the response shape are unchanged. CLI
  list/export/health/doctor are untouched.
- **UI honesty:** copy no longer implies loaded-page-only search; the empty-wall
  first-run guidance is kept distinct from a filter no-results state.

## Validation

gofmt clean; `go vet ./...` clean; `go test ./...` passes incl. new store tests
(text search across title/body/tags/meta, case-insensitive multi-term AND,
type+pinned+query compose, pagination across a filtered interleaved set with no
skip/dup, literal `%_`, empty query == unfiltered, no-match) and a server test
(`q`/`pinned` params + backward-compat + no-match). `npm run lint` + `npm run
build` pass; `make build` embeds the frontend. Browser smoke (headless,
domcontentloaded, explicit selectors — no networkidle): seeded 61 items with the
match as the OLDEST (so it is NOT on the loaded first page of 50); searching for
it surfaced the item, proving full-history search; no-results and clear behaved;
pinned filter drove the server query; 360px mobile had no overflow.
