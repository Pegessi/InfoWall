# 2026-07-02 — Fix: portrait-mode panel height drag non-functional

## Symptom

On tall / narrow (portrait) viewports, dragging a topic panel's height handle
(or using the up/down height step buttons) had no visible effect — the panel
stayed at full viewport height regardless of the value shown in the height
readout.

## Root cause

`topicPanelStyle()` in `web/src/components/feed/FeedList.tsx` rendered:

```ts
height: `min(${PANEL_MAX_HEIGHT}px, max(${column.height}px, calc(100vh - 8.5rem)))`
```

The inner `max(column.height, calc(100vh - 8.5rem))` floors every panel at the
viewport-fill height. On a portrait viewport `100vh - 8.5rem` is large — often
larger than the user's dragged height and up to `PANEL_MAX_HEIGHT` (960px), so
the `max()` silently overrode the user's choice. The drag *did* update
`column.height` (visible in the readout), but the rendered height never dropped
below viewport-fill, so the drag looked broken.

## Fix

`column.height` is already clamped to `[PANEL_MIN_HEIGHT, PANEL_MAX_HEIGHT]`
(360–960) by `clampPanelHeight` in `web/src/lib/feedLayout.ts` wherever it is
set (drag, step buttons, presets, stored-layout parse). So the panel height can
simply render the user value directly:

```ts
function topicPanelStyle(column: TopicColumnPreference): CSSProperties {
  return { height: `${column.height}px` };
}
```

The unused `PANEL_VIEWPORT_FILL_HEIGHT` constant was removed. `PANEL_MAX_HEIGHT`
remains imported/used by the height step buttons.

## Validation

- `npm run lint` (tsc --noEmit) — pass, no errors.
- `npm run build` — pass (pre-existing >500kB chunk-size warning only).
- `make build` — production binary built with embedded frontend.
- Smoke test: served `/`, pushed a note + paper via `infowall push --server`,
  `/api/items` returned 2 items. Confirmed the built JS bundle no longer
  contains `calc(100vh - 8.5rem)` (grep for `8.5rem` → 0 matches).

## Pitfall noted

The height clamp lives in two conceptual layers — JS (`clampPanelHeight`) and
CSS (`topicPanelStyle`). Duplicating a min/floor in the CSS layer overrode the
authoritative JS value. Keep panel-size clamping in one place (the JS layout
helpers) and let the style function render the resolved value verbatim.
