# Claude 0821 primary analyzer

## Scope

- Select the automatic primary analyzer only from local Claude Hub tabs whose
  `ANTHROPIC_MODEL` identifies the 0821 environment.
- Rename the persisted analyzer route and workbench labels to `claude-0821`.
- Expose `--claude-0821-tab` and `INFOWALL_CLAUDE_0821_TAB_ID`; retain the
  previous day1 flag and environment variable only as deprecated aliases.

## Rationale

The active local Claude Hub tabs use the 0821 model environment. The old day1
model-name selector therefore found no primary analyzer and made every
collection fall back to Codex.

## Boundaries

InfoWall remains a read-only consumer of the selected Claude Hub tab's process
environment. It does not alter Hub settings, sessions, tasks, or credentials.
The existing timeout, one clean retry, restricted process permissions, and
Codex fallback behavior are unchanged.

## Validation

Run focused Go and web tests, the full Go suite, lint/build checks, and verify
the built binary exposes the new flag. No production collector is restarted or
scanned as part of this change.
