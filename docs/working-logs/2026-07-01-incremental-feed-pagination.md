# 2026-07-01 — Incremental feed pagination

## Goal

Keep daily browser operation responsive as the local feed grows: the wall should
load a bounded newest page first, then fetch older history on demand without
skipping or duplicating rows when new SSE items arrive between page loads.

## What changed

- **Store/API pagination**
  - Kept the existing `limit`/`offset` API for CLI and older clients.
  - Added keyset pagination via an opaque `cursor` query param on
    `GET /api/items`.
  - Responses now include `has_more` and, when another page exists,
    `next_cursor`.
  - Ordering is deterministic: `pinned DESC, created_at DESC, id DESC`.
- **Browser feed loading**
  - The initial load remains bounded at 50 items.
  - `Load more` now uses the server `next_cursor`, not `items.length`, so
    `item.new` events prepended by SSE do not shift the next history page.
  - Appending older pages deduplicates by item id as an extra guard.
- **Search/filter copy**
  - Search/filter remains client-side over loaded items only.
  - Counts and no-results text now say "loaded" when older history may still be
    available, and the UI shows an end-of-history state when there is no more.

## Compatibility choices

- `offset` stays supported and still behaves as before; `cursor` is a compatible
  extension. If both are supplied, `cursor` wins.
- CLI `list` continues to work because it reads the same `items` array and
  ignores extra response fields when rendering the table. `--json` now exposes
  the server page metadata too.
- No schema migration was introduced. The existing `(pinned, created_at)` index
  remains useful; `id DESC` is a stable tie-break for deterministic page
  boundaries.

## Tradeoffs / residual edge cases

- If an item's pinned state changes between page loads, it can move across the
  ordering boundary. Visible SSE `item.pin` updates still patch the loaded item
  in place; the cursor represents the ordering at the time the previous page was
  fetched.
- Search/filter is intentionally not a full-database search. It is honest about
  loaded scope and can be expanded later with server-side search if needed.

## Validation

Backend tests cover cursor pagination continuing after a newer insert,
same-timestamp id tie-breaks, pinned ordering, HTTP `next_cursor`/`has_more`, and
invalid cursor errors. Frontend validation relies on TypeScript/build gates plus
a browser smoke because there is no frontend test runner in this project.
