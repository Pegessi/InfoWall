# Day1 automatic ingestion on the remote primary

The remote primary retained an enabled scan schedule but was launched with
`--integrations none --disable-background-workers`. Its last successful run was
2026-10-05 23:00 Asia/Shanghai, and the persisted next run remained 10-06 09:00.
Changing the analyzer alone cannot restart scheduling.

InfoWall's Claude preset loader also rejected every model without `0821` in
its name, even when an explicitly named day1 preset had a model and credential.
The loader now accepts any explicitly named preset with both fields, preserves
the restricted ephemeral Claude Code invocation, and reports `claude-day1`
for the day1 profile. The existing 0821 route and legacy serve flag remain
compatible. The UI labels day1 separately and uses generic fallback wording.

Deployment remains opt-in: the Merlin service pack keeps workers disabled and
integrations `none` by default. Operators must enable builtin integrations,
enable workers, and select a private local preset on the service host. A
single manual scan plus a later scheduled run are required for live acceptance;
an old watermark is not evidence that the scheduler is currently running.
