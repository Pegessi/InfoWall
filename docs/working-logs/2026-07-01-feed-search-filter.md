# 2026-07-01 — Feed search & filter (client-side)

## Goal

Daily-use findability for the wall: once enough cards accumulate, let a local
operator quickly narrow the feed (search text, content type, pinned-only)
without losing live-wall behavior — reverse-chron order, topic/stack/board
layouts, layout persistence, pin/delete, and SSE updates.

## Approach

Pure client-side filtering over the already-loaded `items` — no backend, API,
schema, or storage change, and no new state library.

- **`web/src/lib/feedFilter.ts`** (new) — the filter model + logic:
  - `FeedFilter { query, type, pinned }`, `EMPTY_FEED_FILTER`, `isFilterActive`.
  - `availableTypes(items)` — distinct types present, sorted; derived from live
    items so unknown/new types appear automatically (nothing hardcoded).
  - `itemMatches` / `filterItems` — case-insensitive; multi-term AND search over
    a defensively-built haystack: title, body, type, tags, and string values
    found in `meta` (recurses ≤2 levels; ignores non-strings). Missing fields
    never throw.
- **`web/src/components/feed/FeedFilterBar.tsx`** (new) — compact, responsive
  control row using existing styles (CSS vars, `cn()`, lucide icons): search
  input with inline clear, type `<select>` (All + present types), pinned toggle
  (icon-only on mobile via `sm:inline` label), a live `N of M` / `M items`
  count, and a Clear button shown only when a filter is active. Wraps with
  `flex-wrap`; verified no horizontal overflow at 360px.
- **`web/src/components/feed/FeedList.tsx`** — wired filter state in:
  - `filteredItems = filterItems(items, filter)` feeds the existing
    grouping/layout pipeline unchanged, so stack/board/focus views, pin/delete,
    and SSE all keep working (a new SSE item flows through the active filter
    automatically).
  - New `NoResultsState` panel (SearchX + message + Clear) shows when
    `items.length > 0 && filteredItems.length === 0`; the first-run `EmptyState`
    still shows only when there are genuinely zero items. Layout controls hide
    in the no-results case.

## Key decision / subtlety

**Layout persistence must be driven by the UNFILTERED topic set.**
`useFeedLayout(topicIds)` → `resolveLayoutColumns` drops any column whose id is
not in `topicIds` and persists the result to localStorage. If we fed it the
*filtered* topics, then resizing/reordering while a filter is active would erase
saved width/height/order for the temporarily-hidden topics. So `topicIds` is
derived from `availableTypes(items)` (all items), while `groupedItems` (filtered)
decides which panels actually render — `TopicStack`/`TopicBoard` already hide
empty topics. This keeps column prefs intact across filtering.

Filter state is component-local React state and intentionally NOT persisted; a
fresh page load starts unfiltered. Only the pre-existing layout prefs persist.

## Out of scope (untouched)

No backend/API/schema, no full-text DB index, no pagination redesign, no saved
searches, no AI search, no Header/theme changes, no commits. Note: filtering
operates on items currently loaded in the page (initial page + SSE + Load more),
not unloaded pages — honest given the small local model.

README was intentionally NOT edited: a concurrent task (备份恢复演练/runbook) has
large uncommitted README changes, and the search/filter UI is self-evident
in-app; touching README would risk clobbering that work. Revisit if a dedicated
docs pass is wanted later.

## Validation

`npm run lint` (tsc --noEmit) and `npm run build` pass; `make build` embeds the
new frontend. Browser (Playwright/Chromium, headless) against a seeded
multi-type wall confirmed: search narrows live with a correct `N of M` count
(body + tag matches), type filter, pinned-only, Clear/reset, distinct
no-results panel (EmptyState not shown), pin action still works under an active
filter, and no horizontal overflow at 360px mobile. (Headless run required
blocking the external Google-Fonts `<link>`, which otherwise hangs offline and
delays React mount — a test-harness detail, not an app issue.)
