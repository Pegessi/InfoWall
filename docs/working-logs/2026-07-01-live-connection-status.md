# 2026-07-01 — Live SSE connection status & manual retry

## Goal

Give the browser wall a compact, non-intrusive indication of the live SSE
stream's health (connected / connecting / reconnecting / offline) plus a manual
retry, so a daily operator can trust that new cards are actually arriving —
without touching the feed, layouts, search/filter, pin/delete, persistence, or
the EventSource query-key auth flow.

## Approach (frontend-only)

- **`web/src/hooks/useFeed.ts`** — added a `connection: ConnectionState`
  (`connecting | live | reconnecting | disconnected`) plus `retry()`.
  - The SSE setup moved into a single `connect()` callback that **always closes
    any existing `EventSource` before opening a new one** (`esRef`), so there is
    never more than one active stream across initial load, browser auto-retry,
    or manual retry — no duplicate streams or leaks. The effect cleanup still
    closes the stream on unmount.
  - State is driven by real events: `onopen` → `live`; `onerror` → map
    `readyState` (`CLOSED` → `disconnected`, otherwise `reconnecting`, since the
    browser keeps `CONNECTING` while auto-retrying). A stale `onerror` from a
    replaced stream is ignored via an `esRef.current !== es` guard.
  - `fetchLatest()` (reused by initial load and `retry()`) reloads the newest
    page; `retry()` reconnects **and** refetches so anything missed while down
    appears immediately rather than waiting for the next auto-retry.
- **`web/src/components/feed/ConnectionStatus.tsx`** (new) — compact dot + short
  word (Live / Connecting / Reconnecting / Offline) using existing CSS vars
  (`--positive`, `--negative`, `--muted-foreground`; amber-500 for reconnecting
  since there is no warning var). Pulsing dot while connecting/reconnecting.
  `role="status"` + `aria-live="polite"` for a11y. A small Retry button shows
  whenever the stream is unhealthy (`reconnecting` or `disconnected`).
- **`web/src/components/feed/FeedList.tsx`** — pulls `connection` + `retry` from
  `useFeed`; renders `<ConnectionStatus>` inside the `LayoutControls` row (left
  group, via a `status` slot) so it is visible across stack/board layouts, and
  also renders it in the filter no-results branch so status stays visible there.

## Key decisions / pitfalls

- **Retry on `reconnecting`, not just `disconnected`.** A down server keeps the
  EventSource in `CONNECTING` (auto-retry), so it shows as `reconnecting` and
  essentially never reaches `CLOSED`/`disconnected`. Gating Retry only on
  `disconnected` would hide it during the most common outage. Browser-verified:
  stopping the server → "Reconnecting" with Retry visible; clicking Retry after
  restart → "Live" and the item added while offline was refetched.
- **Single stream guarantee** comes from `connect()` closing the prior stream +
  the stale-handler guard; verified one stream per (re)connect in the browser.
- **No "stale" heartbeat detector** added: the server pings every 25s, and a
  timeout-based stale state risks false positives; `onerror`/`readyState` cover
  the real failure modes for this slice.
- Connection state is in-memory only; auth is unchanged (`createEventSource`
  still builds the `?key=` URL).

## Out of scope (untouched)

No backend/auth/websocket/notification/service-worker changes, no visual
redesign, no commits. README left unedited — a concurrent task holds large
uncommitted README changes and the indicator is self-evident in-app.

## Validation

`npm run lint` + `npm run build` pass; `make build` embeds the new frontend.
Playwright/Chromium headless against a seeded server: Live on load; CLI push
arrives live; server stop → Reconnecting + Retry; restart + Retry → Live and
missed item refetched (2→3 cards); one stream per connect; search/filter
regression OK (paper → 1); mobile 360px no horizontal overflow. Desktop +
mobile screenshots reviewed. (Headless run blocks the external Google-Fonts
link, which otherwise hangs offline and delays React mount — harness detail.)
