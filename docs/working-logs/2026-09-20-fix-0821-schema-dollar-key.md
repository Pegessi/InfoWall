# Fix 0821 ingestion: drop `$schema` from the analysis JSON Schema

## Symptom

Scheduled Feishu ingestion frequently fell back from the `claude-0821` primary
analyzer to Codex. The run history in the production workbench DB showed two
recurring primary errors:

- `decode analysis output (invalid-json): output does not match the strict schema`
- occasional `Claude 0821 analysis failed: exit status 1` / 10-minute timeouts

Only a minority of runs completed on `claude-0821`, and those did so
intermittently — it looked flaky rather than broken.

## Root cause

`analysisSchemaFor` (in `internal/feishuingest/codex.go`) hands the model a JSON
Schema whose first keyword is
`"$schema":"https://json-schema.org/draft/2020-12/schema"`.

The Claude Code CLI maps `--json-schema` onto its native **StructuredOutput**
tool input. That tool rejects the JSON-Schema dialect declaration keyword: when
`$schema` is present, the CLI silently does **not** arm native structured output,
so `structured_output` in the JSON envelope comes back `null` and the answer is
emitted as free text in `result`. The 0821 model then only sometimes emits a
bare JSON object; when it wraps the JSON in prose
(e.g. "I've called the StructuredOutput tool…"), `decodeAnalysisResult` fails
with `invalid-json`. Both primary retries fail and the worker falls back to
Codex, which tolerates `$schema` — explaining the intermittent successes.

Reproduced against the real CLI (claude 2.1.159) using the 0821 preset:

- Full schema as generated: `structured_output=null` on 8/8 runs; roughly half
  also produced non-JSON `result` text.
- Identical schema with only the `$schema` key removed: `structured_output`
  populated on every run (verified with enum + `$ref/$defs` + nested objects
  intact, including 4/4 successful classifications of a real-shaped manifest).
- Feature bisection (`$ref/$defs`, `enum`, nested objects, `min/max`,
  `minLength/maxLength`) showed none of those matter; `$schema` alone is the
  trigger.

Credentials, model route, the restricted tool allowlist, and MCP config were all
confirmed healthy independently.

## Fix

`analysisSchemaFor` now `delete(schema, "$schema")` before injecting the
candidate-ID enum. The keyword is purely a dialect declaration: InfoWall
validates output itself in `validateResult`, and Codex does not require it, so
removing it is safe for both analyzers (they share this schema builder).

Added a regression assertion in `codex_test.go` that the generated schema never
contains `$schema`.

## Validation

- `go vet ./internal/...` clean; `go test ./internal/...` all packages pass.
- Production `go build ./cmd/infowall` succeeds (reusing the existing embedded
  frontend; no frontend change).
- Live CLI verification before/after the change as described above.

No production collector was restarted and no data was changed. The fix takes
effect for the running daily server on the next `make build` + restart.
