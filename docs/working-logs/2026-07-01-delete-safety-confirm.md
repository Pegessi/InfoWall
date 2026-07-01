# 2026-07-01 — Delete safety: two-step confirm in the item card

## Goal

Prevent accidental irreversible deletion from the browser wall (persisted local
SQLite data) without breaking the agent-friendly CLI/API delete contract.

## Approach (frontend-only, one file)

`web/src/components/feed/CardHeader.tsx` — the card's hover-revealed actions
previously called `onDelete(item.id)` on a single click. Replaced that with an
inline two-step confirm:

- First click on the trash button **arms** a confirm state (local
  `useState`), swapping the trash icon for a red "✓ Delete" confirm button plus
  an "✕" cancel button.
- Only a second click on the confirm button calls the existing
  `onDelete(item.id)` (unchanged `useFeed.deleteItem` → optimistic remove +
  rollback, and the `item.delete` SSE path).
- A ~3s `setTimeout` silently disarms a forgotten armed card; Cancel and
  `Escape` (keydown on the confirm group) also disarm. The timer is cleared on
  unmount.
- While armed, the actions area is forced visible (not just on `group-hover`)
  so the confirm/cancel controls don't vanish if the pointer drifts; focus moves
  to the confirm button when armed (keyboard/screen-reader friendly). All
  controls are real `<button>`s with descriptive `aria-label`s.

No new dependencies, no global/persisted state, no undo buffer, no soft-delete.
The guard is inherited everywhere automatically because the stack, board, and
topic-focus views all render `ItemCard → CardHeader`.

## What was deliberately NOT changed

- `useFeed.deleteItem`, `web/src/lib/api.ts` `deleteItem`, the `DELETE`
  endpoint, and the CLI `delete` command — all unchanged; CLI stays
  scriptable/non-interactive per the agent-friendly contract.
- README — a concurrent task holds large uncommitted README edits; the two-step
  confirm is self-evident in-app and recovery is already covered by the existing
  "data persistence & backup" / restore-by-copy guidance. Editing README here
  would risk clobbering that work, so it was left alone.

## Validation

`npm run lint` (tsc --noEmit) and `npm run build` pass; `make build` embeds the
new frontend. Playwright/Chromium headless (no networkidle waits — SSE stays
open) against a seeded 3-item wall confirmed: one click arms a Confirm control
and does NOT delete; confirm removes the item (persists); Cancel keeps it and
restores the trash button; ~3s auto-reset disarms without deleting; pin and
search/filter still work; 360px mobile shows no horizontal overflow. Armed-state
desktop + mobile screenshots reviewed; temp server/files cleaned up.
