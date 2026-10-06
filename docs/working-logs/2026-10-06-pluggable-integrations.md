# Pluggable integration boundary

## Goal

Separate InfoWall's durable product core from vendor-specific collection,
enrichment, analysis, and publication behavior without changing how existing
users store or access their workbench.

## Design

The core remains responsible for SQLite persistence, the HTTP/SSE API,
authentication and authorization, and business state for feed items, projects,
demands, progress, sources, and reviews. The integration package defines four
vendor-neutral roles around that boundary:

- a **connector** collects normalized observations from an external source;
- an **enricher** resolves resources referenced by those observations;
- an **analyzer** proposes demand, progress, and review changes using a bounded
  snapshot of core state; and
- an **exporter** publishes a workbench snapshot to an external destination.

Each component supplies a stable descriptor and uses versioned neutral request
and result types. Component IDs are unique within one composition. Connectors
require an analyzer, while the zero-component composition is explicitly valid.
That lets InfoWall run as a standalone SQLite/HTTP application with no external
system configured.

## Compatibility decisions

Existing Feishu collection/enrichment and document publication, Codebase
enrichment, and Claude/Codex analysis remain available through built-in
compatibility adapters. This preserves user-facing behavior and makes the
vendor-specific implementation explicit instead of presenting it as the core
architecture. Earlier working logs that discuss those products describe the
history of these compatibility adapters and are intentionally left unchanged.

Deployment environments are not integrations. Environment-specific service
wrappers, network publication mechanisms, host settings, and credentials live
outside this repository and consume InfoWall only through its documented
binary and HTTP interfaces.

## Compatibility and migration

This phase changes neither the SQLite schema, HTTP API, SSE events, nor stored
business records. The existing built-in composition is adapted to the neutral
contracts so installations can migrate incrementally. The CLI adds
`serve --integrations builtin|none` (and `INFOWALL_INTEGRATIONS`) as the explicit
composition switch. `builtin` remains the default, so existing invocations are
unchanged; `none` passes an explicit empty component set to the server. A
server with zero adapters remains a supported configuration rather than a
degraded or invalid mode.

Worker scheduling is a separate concern. `--disable-background-workers`
pauses automatic workers but does not remove built-in adapters or disable their
on-demand compatibility endpoints.

## Current limits

The contract is a Go API implemented and composed in-process at server
construction time. It does not provide an external HTTP transport, executable
plugin discovery, runtime installation, dynamic loading, or hot reload. Adapter
configuration and credentials remain process-local concerns and are not stored
in the core business schema.

`--integrations none` is a runtime composition boundary, not a source-code or
binary-size boundary. The built-in compatibility adapter implementations still
live in this module and are compiled into the binary in this phase. This is
intentional: those adapters are supported product capabilities, while private
deployment packaging has been physically removed from this repository. No live
credentials are intentionally stored in the current tree.

## Next phase

A later phase may define an out-of-process HTTP contract. Before implementation
it must specify authentication, capability and protocol-version negotiation,
timeouts and cancellation, retry and acknowledgement rules, idempotency,
checkpoint ownership, error and warning envelopes, lifecycle/health behavior,
and secret/config distribution. That work must preserve the same neutral data
model rather than exposing vendor-specific types across the boundary.
