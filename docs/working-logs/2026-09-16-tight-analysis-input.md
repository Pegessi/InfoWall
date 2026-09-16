# Tighten automatic analysis input and output protocol

## Why

The activity analyzer previously placed every existing demand into the
temporary `demands/` directory. A small three-candidate run exposed 635 files,
which made the read-only agent's search space unnecessarily broad and worsened
latency and strict-output compliance.

## Change

- Keep candidate batches unchanged, but serialize at most 100 existing demands.
- Rank exact source/resource identities above lexical anchors. The lexical
  fallback supports mixed Chinese and ASCII component names and is deterministic
  by demand ID on ties.
- State an output protocol before all classification rules: one raw JSON object,
  no prose/fences/envelope, all top-level arrays present, and a final candidate
  coverage check.

## Boundaries

This does not relax output parsing or permit JSON repair. Invalid output still
fails closed and uses the existing clean retry then Codex fallback. It does not
change watermarks, source permissions, or demand mutation semantics.

## Validation

- `go test ./...`
- `npm run lint`
- `npm run test --if-present`
- Added coverage for stable-identity selection, lexical fallback, deterministic
  100-demand cap, and output-protocol wording.
