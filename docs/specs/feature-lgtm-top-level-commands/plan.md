---
type: feature-plan
title: "Top-Level LGTM Signal Commands"
status: draft
spec: docs/specs/feature-lgtm-top-level-commands/spec.md
created: 2026-04-01
---

# Architecture and Design Decisions

## Pipeline Architecture

### Current Structure

```
gcx
├── datasources
│   ├── list
│   ├── get
│   ├── generic          ← auto-detect query
│   │   └── query
│   ├── prometheus       ← LGTM signal buried here
│   │   ├── query
│   │   ├── labels
│   │   ├── metadata
│   │   └── targets
│   ├── loki
│   │   ├── query
│   │   ├── labels
│   │   └── series
│   ├── tempo
│   │   └── query
│   └── pyroscope
│       ├── query
│       ├── labels
│       ├── profile-types
│       └── series
├── adaptive             ← unified provider (registered via providers.Register)
│   ├── metrics
│   │   ├── rules {show,sync}
│   │   └── recommendations {show,apply}
│   ├── logs
│   │   ├── patterns {show}
│   │   ├── exemptions {show,create,delete}
│   │   └── segments {show,create,delete}
│   └── traces
│       └── policies {show,create,update,delete}
└── [other providers...]
```

### Target Structure

```
gcx
├── datasources
│   ├── list             ← unchanged
│   ├── get              ← unchanged
│   └── query            ← renamed from "generic"
├── metrics              ← NEW provider (registered via providers.Register)
│   ├── query            ← moved from datasources prometheus query
│   ├── labels           ← moved from datasources prometheus labels
│   ├── metadata         ← moved from datasources prometheus metadata
│   ├── targets          ← moved from datasources prometheus targets
│   └── adaptive
│       ├── rules {show,sync}
│       └── recommendations {show,apply}
├── logs                 ← NEW provider
│   ├── query            ← moved from datasources loki query
│   ├── labels           ← moved from datasources loki labels
│   ├── series           ← moved from datasources loki series
│   └── adaptive
│       ├── patterns {show}
│       ├── exemptions {show,create,delete}
│       └── segments {show,create,delete}
├── traces               ← NEW provider
│   ├── query            ← moved from datasources tempo query
│   └── adaptive
│       └── policies {show,create,update,delete}
├── profiles             ← NEW provider
│   ├── query            ← moved from datasources pyroscope query
│   ├── labels           ← moved from datasources pyroscope labels
│   ├── profile-types    ← moved from datasources pyroscope profile-types
│   ├── series           ← moved from datasources pyroscope series
│   └── adaptive         ← stub (prints "not yet available" to stderr)
└── [other providers unchanged...]
```

### Provider Registration Flow

```
internal/providers/{signal}/provider.go
    │
    ├── init() → providers.Register(&Provider{})
    │               ├── adds to provider registry
    │               └── auto-registers TypedRegistrations()
    │
    └── Commands() → []*cobra.Command
            │
            ├── datasource subcommands (reuse existing constructors from
            │   cmd/gcx/datasources/*.go and cmd/gcx/datasources/query/*.go)
            │
            └── adaptive subcommands (reuse existing functions from
                internal/providers/adaptive/{signal}/commands.go)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Each signal provider lives in `internal/providers/{signal}/` | Follows existing provider pattern (slo, fleet, oncall). Each provider is a self-contained package with init() self-registration. (FR-001 through FR-004) |
| Datasource command constructors (labels, metadata, targets, series, profile-types) are extracted to exported functions in `cmd/gcx/datasources/` files | NC-001 prohibits business logic duplication. The new providers call these same constructors, just mounting them under a different parent. |
| The `cmd/gcx/datasources/query/` package constructors (PrometheusCmd, LokiCmd, etc.) are already exported | No extraction needed — signal providers import and call them directly. |
| Adaptive signal subpackages stay at `internal/providers/adaptive/{signal}/` | The signal subpackages already live under providers. The new signal providers import from `internal/providers/adaptive/{signal}/commands.go`. Auth stays at `internal/providers/adaptive/auth/` as a shared utility (NC-007). |
| ConfigKeys split per signal — each provider owns only its signal's tenant-id and tenant-url | FR-024 requires appropriate ConfigKeys. Metrics gets metrics-tenant-id/url, logs gets logs-*, traces gets traces-*. Profiles gets none (no adaptive backend yet). |
| TypedRegistrations split per signal — logs provider returns exemption+segment registrations, traces provider returns policy registrations, metrics+profiles return nil | FR-025 — maintains current registration behavior, just distributed across providers. |
| `datasources generic` renamed to `datasources query` by changing Use field and updating function name | FR-022 — simplest approach, single-line change plus rename. |
| profiles adaptive stub prints to stderr and returns nil (no error) | NC-008 — stub MUST print to stderr only, not stdout. |
| Each signal provider binds ConfigLoader persistent flags on its root command | Matches existing adaptive provider pattern — loader.BindFlags on root persistent flags, inherited by all subcommands. |

## Compatibility

| Area | Status |
|------|--------|
| `gcx datasources list` | Unchanged (FR-028) |
| `gcx datasources get` | Unchanged (FR-028) |
| `gcx datasources query` (was `generic`) | Renamed, same behavior (FR-022, FR-028) |
| `gcx datasources prometheus` | Removed (FR-021, NC-002) |
| `gcx datasources loki` | Removed (FR-021, NC-002) |
| `gcx datasources tempo` | Removed (FR-021, NC-002) |
| `gcx datasources pyroscope` | Removed (FR-021, NC-002) |
| `gcx adaptive` | Removed (FR-023, NC-002) |
| `gcx metrics`, `gcx logs`, `gcx traces`, `gcx profiles` | New (FR-001 through FR-004) |
| `gcx providers list` | Shows new signal providers (FR-029) |
| Provider interface | Unchanged (NC-004) |
| Config keys | Unchanged values, distributed across providers (NC-005) |
| Adaptive auth package | Unchanged location, shared (NC-007) |
