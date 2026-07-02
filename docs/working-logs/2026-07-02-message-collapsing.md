# 2026-07-02 — Message Collapsing

## What changed

Added per-item collapse/expand to the feed so the wall defaults to a scannable
title-only view for older content.

- **Pinned items** and **today's items** always render fully expanded.
- **Older non-pinned items** render as compact title cards by default (icon,
  title, relative time only — no body, no tags/hostname).
- Clicking a collapsed card expands it to show full content; clicking an
  expanded collapsible card collapses it back. A chevron indicator
  (right = collapsed, down = expanded) signals the affordance.
- When a search/filter is active or the user is in a topic focus view, all
  items render expanded (the user is in a focused browsing mode).
- Pinning a collapsed item auto-expands it (pin is an explicit "keep visible"
  signal).
- Keyboard accessible: collapsible cards are `role="button"`, support Enter/Space
  to toggle, and have `aria-expanded`.

## Files changed

- `web/src/lib/time.ts` — added `isToday()` helper using `date-fns/isSameDay`.
- `web/src/components/feed/ItemCard.tsx` — added `defaultCollapsed` prop,
  internal toggle state, chevron indicator, compact vs expanded styling,
  click-to-toggle with keyboard support.
- `web/src/components/feed/CardHeader.tsx` — added `compact` prop for collapsed
  cards (smaller text, tighter spacing, hides tags/hostname/type label, shows
  only title + relative time). All interactive elements (links, pin, delete)
  call `stopPropagation` so they don't trigger card toggle.
- `web/src/components/feed/FeedList.tsx` — added `collapseOlder` prop to
  `TopicStack` and `TopicBoard`; passes `defaultCollapsed` computed as
  `collapseOlder && !item.pinned && !isToday(item.created_at)`. When filtering
  or in topic focus view, `collapseOlder` is false so all items expand.

## Design decisions

- **No persistence**: collapse state is per-session client state only. A page
  reload resets to the default (pinned + today expanded, older collapsed).
- **No "expand all / collapse all" toggle**: can be added later if needed; the
  per-item click covers the common case.
- **Click-to-toggle on the whole card** (not just the chevron) for quick
  scanning, but body content and action buttons stop propagation so interacting
  with links/content doesn't accidentally collapse.
- **Compact header** strips tags, hostname, and type label to keep the title
  card truly scannable; relative time remains for orientation.
- **ChevronRight** (not ChevronDown) for collapsed state to suggest "drill in"
  rather than "open a dropdown"; ChevronDown for expanded matches common
  accordion patterns.
