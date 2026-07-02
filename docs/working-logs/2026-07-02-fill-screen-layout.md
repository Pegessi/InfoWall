# 2026-07-02 — Default topic board fills screen width & height

## Task
显示bug (task 6c078716): the default topic-board (Balanced) layout did not fill
the browser window — columns used fixed pixel widths that summed to less than a
wide viewport (empty gap on the right) and panels used a fixed 600px height
(empty band below).

## Root cause
`web/src/components/feed/FeedList.tsx` `TopicBoard` rendered the CSS grid with
`gridTemplateColumns: "<w1>px <w2>px …"` and each panel with a fixed
`height: <h>px`. The outer `<main>` is `w-full`, so the space was available; the
grid/panel sizing simply never consumed it.

A prior task (b7d295ca 竖屏高度拖拽无用) had deliberately removed a
`max(height, calc(100vh - 8.5rem))` floor because it applied even after a manual
drag and on portrait viewports, silently overriding the height controls. So the
fix could not simply reinstate a viewport-fill floor unconditionally.

## Fix
Gate a "fill to screen" mode on whether the layout is an **untouched preset**
(`layout.presetId !== CUSTOM_PRESET_ID`). Any manual width/height drag or step
already flips `presetId` to `"custom"` (see `useFeedLayout.ts`), so:

- **Preset (default) → fill.** Grid tracks become `minmax(<width>px, 1fr)` so
  columns keep their width as a lower bound but share leftover width to span the
  container; the grid div gets `w-full`. Panels use
  `height: calc(100vh - 9rem)` with `minHeight: PANEL_MIN_HEIGHT` as a floor.
- **Custom (after any manual resize) → fixed pixels.** Grid tracks are
  `<width>px` and panels use the stored pixel height, exactly as before — so the
  b7d295ca height-drag fix is preserved.

`TopicStack` (mobile `lg:hidden` + single-column timeline mode) intentionally
keeps fixed pixel height (`topicPanelStyle(column, false)`); filling each stacked
panel to a full viewport would be worse, and stacked layout was out of scope.

`9rem` (BOARD_VERTICAL_CHROME) approximates header + filter bar + layout
controls + gaps; it need not be pixel-perfect because of the min-height floor and
the panel's own internal `overflow-y-auto`.

## Validation
- `npm run lint` (tsc --noEmit) — pass.
- `npm run build` — pass.
- `make build` — production binary built with embedded frontend.
- `go vet ./...` clean; `go test ./...` all pass (frontend-only change).
- Verified the shipped bundle contains `minmax(${…}px, 1fr)` and the `9rem`
  fill-height constant.
- Logic assertions (mirroring the two pure decisions) confirm: preset →
  minmax/1fr + calc(100vh) with 360px floor; custom → fixed px width + px height.

## Pitfall discovered
Headless chromium in the agent sandbox does not mount the React app (blank root,
no console error, `load` never fires because the SSE `/events` stream stays
open). The unmodified production instance on :8899 behaves identically, so it is
an environment limitation, not a regression. Visual confirmation in a real
browser is left to human review; correctness here was verified via bundle
inspection + logic assertions instead.

## Files
- `web/src/components/feed/FeedList.tsx`

## Review round 2 — drag/step jump-on-first-interaction fix

Reviewer (iw-reviewer-1, attempt 2) approved the fill logic but flagged a real
UX defect: in fill mode the panel is rendered stretched (full-width column,
`calc(100vh - 9rem)` tall), but `column.width`/`column.height` still hold the
preset pixel values. The drag/step handlers seeded their start value from those
stored values, so the *first* drag/click snapped the panel from its stretched
size to `preset ± delta` — a large visual jump.

Fix: seed the interaction from the **actual rendered dimension**, read from the
panel element via `event.currentTarget.closest('[data-topic-panel]')` +
`getBoundingClientRect()`, falling back to the stored value if the element is
absent. Applied to:
- `beginWidthResize` (board) — start from rendered width.
- `beginHeightResize` (board and stack) — start from rendered height.
- New `stepPanelHeight` helper wired to the board's height +/- step buttons —
  steps from the rendered height. (Width has no step button; the stack is never
  in fill mode so its numbers already matched, but the same robust code path is
  used there for the height drag.)

Verified with logic assertions (mirroring the seed computation): fill-mode drag
of −40px from a rendered 940px yields 900px (continuous), whereas the old
stored-value path would have produced 560px (the jump). Custom mode is
unchanged (rendered == stored). `npm run lint`, `npm run build`, `make build`,
`go vet`, and `go test ./...` all pass again.
