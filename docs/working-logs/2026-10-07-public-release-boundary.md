# Public release repository boundary

## Goal

Make the public source tree describe a portable product rather than one
installation, while retaining the optional integrations that are part of the
shipped product.

## Boundary decision

Environment-specific deployment packaging was physically split from this
repository. Host wrappers, private topology assumptions, and operator-specific
service settings belong in a separately controlled deployment pack. This
repository documents only the generic remote-primary model: one authoritative
SQLite service, an operator-managed HTTPS boundary, API-key authentication,
explicit persistent storage, and verified backups.

Local deployment values must remain in ignored or external configuration. The
public tree must not encode a real endpoint, host alias, assigned port, absolute
server path, secret location, or statement about which installation is
currently authoritative.

## What remains public

Feishu collection and publication, Codebase enrichment, and Claude/Codex
analysis are product capabilities and remain as optional built-in compatibility
adapters. Their inclusion does not make them core dependencies: a zero-adapter
server remains valid and retains SQLite, HTTP/SSE, authentication, and all
manual workbench operations.

The neutral connector, enricher, analyzer, and exporter contracts are also
public. In this phase they are composed in-process; no external HTTP plugin
transport, dynamic loading, or hot reload is promised.

## Release checks

Before publishing, scan the complete tracked tree for organization-specific
deployment names, endpoints, filesystem paths, port assignments, credentials,
and private repository references. Also review Git history independently: tree
cleanup does not remove earlier committed content. Rotate any credential that
might have appeared in prior commits even when the current tree contains no
secret value.
