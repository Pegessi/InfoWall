# Bound the 0821 analysis payload so large ingestion windows stop timing out

Follow-up to `2026-09-20-fix-0821-schema-dollar-key.md`. After the `$schema`
fix made native structured output engage, scheduled windows that contained many
candidates still fell back to Codex. The production run history showed two new
failure modes (times in UTC, 2026-09-20):

- `03:30` window (137 messages / 15 candidates): `Claude 0821 analysis timed
  out after 10m0s`, fallback cumulative input ~820k tokens.
- `02:30` and `01:00` windows: `candidate message "…" has no classification
  outcome` — one missed classification failed the whole run.

## Root cause A: serial Read fan-out + over-broad demand preselection

`writeAnalysisWorkspace` wrote each preselected existing demand to its own file
(up to 100). The analyzer reads the manifest then has to open each demand file,
so a run paid ~100 serial tool round-trips before it could answer. Measured
against the real CLI on a synthetic worst case: **100 demand files = 119 turns /
255 s for one attempt**, and the FallbackAnalyzer retries the primary once
before Codex, which pushed the 10-minute budget over.

The preselection selected ~100 demands because lexical scoring used CJK bigrams
as anchors with a `score > 0` threshold. Generic bigrams (需求/进度/接口/模块/问题)
appear across most of the 650-demand backlog, so nearly everything qualified.
The largest 100 selected demands serialized to ~600 KB (150–200k tokens).

## Root cause B: one missed classification aborted the run

`validateResult` required every candidate to appear in some source / skip /
missing-context list and returned an error otherwise. The model occasionally
omits one candidate despite the protocol; a single omission failed, retried, and
fell back the entire window.

## Changes

All in `internal/feishuingest/`.

1. **Inline existing demands into `manifest.json`** (`existing_demands`) instead
   of one-file-per-demand. The analyzer now reads one manifest plus the small
   candidate batch files. Manifest schema key `demand_files` removed; prompt
   updated to match.
2. **Tighten lexical preselection.** Compute each anchor's document frequency
   across the whole backlog and ignore anchors present in more than
   `distinctiveAnchorMaxDF` (3) demands. Require either ≥2 rare ASCII
   identifier hits (component/model/resource tokens such as `m15`/`cuda`) or
   ≥4 total rare hits (CJK bigrams collide readily, especially in long agent
   transcripts). Stable resource/source identities remain decisive. Hard cap
   lowered 100 → `analysisDemandLimit` (40). Calibrated against the real
   personal-workbench snapshot: generic-only candidate preselects 0; a real
   long-transcript window drops from 104 raw lexical matches to ~27.
3. **Default uncovered candidates to skipped.** `validateResult` now appends any
   uncovered candidate IDs (sorted) to `skipped_message_ids` instead of
   erroring. Skipped IDs are never persisted as demands, so this is the safe
   deterministic default; genuine structural errors still fail.
4. **Single-attempt timeout 10m → 15m** (`defaultAnalysisTimeout`, shared by
   the Claude and Codex analyzers) as margin. The in-process worker lock still
   prevents overlapping runs.

## Validation

- `go vet ./...`, `gofmt`, and `go test ./internal/...` all pass; new/updated
  unit tests cover generic-anchor rejection, rare-ASCII matching, the
  deterministic identity cap, the inlined manifest, and the skip default.
- End-to-end against the real 0821 CLI on a workspace rebuilt from a
  VACUUM-copy of the production DB (read-only; production DB untouched):
  before shape = 119 turns / 255 s (100 demand files); after = **12 turns /
  200 s** on a 9-candidate/40-inlined-demand real window, structured output
  present, all 9 candidates covered (1 progress / 7 skipped / 1 missing
  context). The 200 s is dominated by reading a ~220 KB manifest once; the
  tightened selector shrinks that further in production.

No production collector was restarted and no data changed for this change.
