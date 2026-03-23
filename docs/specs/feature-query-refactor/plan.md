---
type: feature-plan
title: "Move query under datasources with per-kind subcommands"
status: draft
spec: spec/feature-query-refactor/spec.md
created: 2026-03-18
---

# Architecture and Design Decisions

## Pipeline Architecture

### Before

```
cmd/grafanactl/root/command.go
├── rootCmd.AddCommand(query.Command())        ← top-level "query"
├── rootCmd.AddCommand(datasources.Command())
│   ├── list, get
│   ├── prometheus (labels, metadata, targets)
│   ├── loki (labels, series)
│   └── pyroscope (profile-types, labels)
└── ...

cmd/grafanactl/query/
├── command.go   ← monolithic: all kinds, all flags, type auto-detect
├── graph.go     ← graph codec
├── time.go      ← time parsing (ParseTime, ParseDuration)
└── time_test.go
```

Default datasource resolution is duplicated inline across:
- `cmd/grafanactl/query/command.go` (ad-hoc multi-field check)
- `cmd/grafanactl/datasources/prometheus.go` (3 commands, each inline)
- `cmd/grafanactl/datasources/loki.go` (2 commands, each inline)
- `cmd/grafanactl/datasources/pyroscope.go` (2 commands, each inline)
- `internal/providers/synth/checks/status.go` (custom 4-tier resolution)

### After

```
cmd/grafanactl/root/command.go
├── rootCmd.AddCommand(datasources.Command())
│   ├── list, get
│   ├── prometheus (labels, metadata, targets)
│   ├── loki (labels, series)
│   ├── pyroscope (profile-types, labels)
│   └── query                              ← NEW group command
│       ├── prometheus                     ← typed subcommand
│       ├── loki                           ← typed subcommand
│       ├── pyroscope                      ← typed subcommand
│       ├── tempo                          ← stub subcommand
│       └── generic                        ← auto-detect subcommand
└── ...
(NO rootCmd.AddCommand(query.Command()) — removed)

cmd/grafanactl/datasources/query/        ← NEW package
├── command.go       ← query group + shared helpers
├── prometheus.go    ← prometheus subcommand
├── loki.go          ← loki subcommand
├── pyroscope.go     ← pyroscope subcommand
├── tempo.go         ← tempo stub
├── generic.go       ← generic auto-detect
├── codecs.go        ← table/wide/graph codecs (moved from old query pkg)
├── time.go          ← ParseTime, ParseDuration (moved from old query pkg)
└── time_test.go     ← time tests (moved)

internal/config/
├── types.go         ← Context gains Datasources map[string]string field
├── resolver.go      ← NEW: DefaultDatasourceUID(ctx Context, kind string) string
└── resolver_test.go ← NEW: tests for precedence logic
```

### Shared Resolver Flow

```
DefaultDatasourceUID(ctx, "prometheus")
  │
  ├─ 1. ctx.Datasources["prometheus"] non-empty? → return it
  │
  ├─ 2. legacy field for kind:
  │     "prometheus" → ctx.DefaultPrometheusDatasource
  │     "loki"       → ctx.DefaultLokiDatasource
  │     "pyroscope"  → ctx.DefaultPyroscopeDatasource
  │     non-empty?   → return it
  │
  └─ 3. return "" (caller decides error message)
```

All consumers (query subcommands, datasource subcommands, synth provider) call this single function.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| New package `cmd/grafanactl/datasources/query/` rather than inlining in `cmd/grafanactl/datasources/` | The datasources package already has 4 files; adding 8+ query files would make it unwieldy. A sub-package keeps command groups cleanly separated. (FR-001, FR-002) |
| Move time utilities into the new query package | `ParseTime` and `ParseDuration` are only used by query commands. Keeping them co-located avoids an unnecessary shared utility package. (FR-007) |
| Move codecs (table, wide, graph) into `codecs.go` in the new package | Codecs are query-specific and type-switch on query response types. No other commands use them. (FR-010) |
| Shared `DefaultDatasourceUID` in `internal/config/resolver.go` | Centralizes the 2-tier precedence logic (new section > legacy key). Eliminates 10+ inline resolution blocks across the codebase. (FR-023, FR-024) |
| `Datasources` field as `map[string]string` on Context struct | Matches the YAML structure `datasources: {kind: uid}`. The existing config editor already handles map traversal via reflection, so `config set contexts.X.datasources.prometheus UID` works with zero editor changes. (FR-020, FR-021) |
| Each typed subcommand validates ds type via API call in RunE | Type validation is a runtime concern requiring an API call. Doing it in RunE keeps Validate() focused on flag/arg validation. (FR-006) |
| `--window` parsed in Validate() to set From/To | Mutual exclusion with `--from`/`--to` is a static validation concern. Converting `--window` to from/to early simplifies the RunE logic. (FR-008) |
| `generic` MUST require UID positional arg (no default resolution) | Avoids ambiguity when multiple defaults are configured; keeps `generic` as an explicit escape hatch. (FR-017) |
| Synth provider adopts shared resolver but retains its own provider-cache and auto-discovery tiers | The shared resolver covers tiers 1-2 (new config + legacy key). Synth's tiers 3-4 (provider cache + API discovery) are provider-specific and remain in `status.go`. Synth calls the shared resolver first, then falls back to its own tiers. (FR-023) |

## Compatibility

| Area | Status |
|------|--------|
| `grafanactl datasources list` / `get` | Unchanged |
| `grafanactl datasources prometheus labels/metadata/targets` | Unchanged behavior; internal resolution migrated to shared resolver (FR-024) |
| `grafanactl datasources loki labels/series` | Unchanged behavior; internal resolution migrated to shared resolver (FR-024) |
| `grafanactl datasources pyroscope profile-types/labels` | Unchanged behavior; internal resolution migrated to shared resolver (FR-024) |
| `grafanactl query` (top-level) | REMOVED — returns "unknown command" error |
| `internal/query/{prometheus,loki,pyroscope}/` client packages | No changes to public API |
| Config: `default-{kind}-datasource` keys | Still functional as fallback; new `datasources.{kind}` takes precedence |
| Config: `datasources` section | NEW — `map[string]string` on Context |
| `internal/providers/synth/checks/status.go` | Migrated to use shared resolver for tiers 1-2 |
