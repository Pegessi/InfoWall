# 2026-06-29 Topic Panels

## What changed

- The feed now defaults to topic panels instead of a flat timeline.
- Items with the same topic are grouped into one fixed-height panel, ordered newest first.
- Each topic panel has an internal scroll area, bottom-edge height resizing, header height controls, and a focused topic view opened from the header/detail button.
- The grouped card view hides the redundant per-item type label so the panel title shows the topic and each item shows its own title.
- The CLI now exposes `--topic` / `-t` as the user-facing way to set or filter an item's topic while keeping `--type` and the JSON `type` field for compatibility.

## Design decisions

- Topic is the product-facing term; type remains the stored/API field to avoid breaking existing database rows, SSE payloads, and JSON clients.
- The browser stores topic panel order, widths, heights, mode, and preset selection in local storage so a user's layout survives refreshes.
- Fixed panel heights are clamped to a practical range to prevent unusably tiny panels or runaway layouts.
- Existing frontmatter with either `type:` or `topic:` is not overwritten by CLI flags.

## Validation notes

- `go test ./...`
- `npm run lint`
- `make build`
- Playwright smoke check against the rebuilt embedded server verified topic grouping, internal scroll, bottom-edge height drag, and focused topic view.
