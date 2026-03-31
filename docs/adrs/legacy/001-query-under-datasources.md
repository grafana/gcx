# Move query under datasources with per-kind subcommands

**Created**: 2026-03-19
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

`gcx query` was a top-level command but semantically belongs under
`gcx datasources`, consistent with how other resource-scoped operations
are organized. Additionally, known datasource kinds (Prometheus, Loki, Pyroscope,
Tempo) each have distinct query languages and semantics that benefit from
dedicated subcommands rather than a generic catch-all.

## Decision

Restructure the query command tree as:

```
gcx datasources query
  prometheus <UID> '<EXPR>' [--from] [--to] [--window]
  loki       <UID> '<EXPR>' [--from] [--to] [--window]
  tempo      <UID> '<EXPR>' [--from] [--to] [--window]
  pyroscope  <UID> '<EXPR>' [--from] [--to] [--window]
  generic    <UID> '<EXPR>'   # escape hatch for community/other datasources
```

Design rationale:

1. Known datasource kinds map to known query types, making it easier to
   construct correct syntax (PromQL vs LogQL vs profile queries)
2. Per-kind subcommands can pre-filter datasource UIDs so users don't
   accidentally query logs from a Prometheus datasource
3. `generic` provides an escape hatch for community and other datasources
4. The structure is extensible — new kinds like SQL can be added later

## Consequences

- The `gcx query` top-level command is removed
- All query functionality moves under `gcx datasources query <kind>`
- Each kind subcommand can implement kind-specific flags and validation
- The `generic` subcommand accepts any datasource type without validation
