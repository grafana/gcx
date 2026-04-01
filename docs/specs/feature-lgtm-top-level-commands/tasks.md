---
type: feature-tasks
title: "Top-Level LGTM Signal Commands"
status: draft
spec: docs/specs/feature-lgtm-top-level-commands/spec.md
plan: docs/specs/feature-lgtm-top-level-commands/plan.md
created: 2026-04-01
---

# Implementation Tasks

## Dependency Graph

```
T1 (extract datasource constructors) ──┬──→ T3 (metrics provider)
                                        ├──→ T4 (logs provider)
                                        ├──→ T5 (traces provider)
                                        └──→ T6 (profiles provider)
                                                     │
T2 (rename generic → query) ────────────┐            │
                                        ├──→ T7 (cleanup: remove old commands + adaptive provider)
T3 ─────────────────────────────────────┤
T4 ─────────────────────────────────────┤
T5 ─────────────────────────────────────┤
T6 ─────────────────────────────────────┘
                                                     │
                                        T7 ──→ T8 (agent annotations + docs)
```

## Wave 1: Shared Infrastructure

### T1: Extract Datasource Command Constructors to Exported Functions
**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

The datasource-specific command constructors in `cmd/gcx/datasources/` (prometheus.go, loki.go, pyroscope.go, tempo.go) use unexported functions that accept `*cmdconfig.Options`. The new signal providers live in `internal/providers/` and need to call these same constructors.

Extract the leaf command constructors (labelsCmd, metadataCmd, targetsCmd, lokiLabelsCmd, seriesCmd, profileTypesCmd, pyroscopeLabelsCmd) into exported functions. The existing datasource parent commands continue to call the same functions. The `cmd/gcx/datasources/query/` constructors (PrometheusCmd, LokiCmd, TempoCmd, PyroscopeCmd) are already exported and may need minimal adaptation for the new provider pattern.

This task MUST NOT change any observable CLI behavior.

**Deliverables:**
- `cmd/gcx/datasources/prometheus.go` — export leaf constructors
- `cmd/gcx/datasources/loki.go` — export leaf constructors
- `cmd/gcx/datasources/pyroscope.go` — export leaf constructors
- `cmd/gcx/datasources/query/prometheus.go` — adapt if needed
- `cmd/gcx/datasources/query/loki.go` — adapt if needed
- `cmd/gcx/datasources/query/tempo.go` — adapt if needed
- `cmd/gcx/datasources/query/pyroscope.go` — adapt if needed

**Acceptance criteria:**
- GIVEN the exported constructors exist WHEN called THEN they return identical cobra.Command trees as the original unexported functions
- GIVEN existing `gcx datasources prometheus labels` command WHEN executed with identical flags THEN output is byte-identical to before this change (NC-003)
- GIVEN `go build ./...` and `go test ./...` WHEN run THEN both pass with zero errors

---

### T2: Rename datasources generic to datasources query
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Rename the `gcx datasources generic` subcommand to `gcx datasources query`. Change the `Use` field from `"generic"` to `"query"`, update function names, update Short/Long descriptions, and update `command.go` to call the renamed function. Remove the old `generic` name entirely (no alias).

**Deliverables:**
- `cmd/gcx/datasources/generic.go` — rename to `query_cmd.go` (or update Use field in-place)
- `cmd/gcx/datasources/command.go` — reference renamed function

**Acceptance criteria:**
- GIVEN `gcx datasources query --help` WHEN executed THEN help text displays with Use "query" and describes auto-detecting datasource type (FR-022)
- GIVEN `gcx datasources generic` WHEN executed THEN command is not found (NC-002)
- GIVEN `gcx datasources list` and `gcx datasources get` WHEN executed THEN behavior is identical to before (FR-028, NC-006)

---

## Wave 2: Signal Providers (Parallel)

### T3: Implement metrics provider
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `internal/providers/metrics/provider.go` implementing the Provider interface. Name: "metrics", ShortDesc: "Query Prometheus datasources and manage Adaptive Metrics". Commands() returns a single `metrics` root command with:
- `query` — reuses PrometheusCmd constructor from T1
- `labels` — reuses Prometheus labels constructor from T1
- `metadata` — reuses Prometheus metadata constructor from T1
- `targets` — reuses Prometheus targets constructor from T1
- `adaptive` — parent command containing the adaptive metrics subtree (reuses `internal/providers/adaptive/metrics.Commands()`)

ConfigKeys returns `[]ConfigKey{{Name: "metrics-tenant-id"}, {Name: "metrics-tenant-url"}}`.
TypedRegistrations returns nil (adaptive metrics has no typed registrations).
Validate returns nil.

Bind `ConfigLoader` persistent flags on the `metrics` root command so all subcommands inherit config flags.

**Deliverables:**
- `internal/providers/metrics/provider.go`

**Acceptance criteria:**
- GIVEN `gcx metrics query --help` WHEN executed THEN help text shows query subcommand with `gcx metrics query` examples (FR-001, FR-027)
- GIVEN `gcx metrics labels -d <uid>` WHEN executed THEN output is identical to former `gcx datasources prometheus labels -d <uid>` (FR-009)
- GIVEN `gcx metrics adaptive rules show` WHEN executed THEN output is identical to former `gcx adaptive metrics rules show` (FR-017)
- GIVEN `gcx providers list` WHEN executed THEN "metrics" appears in the list (FR-029)

---

### T4: Implement logs provider
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `internal/providers/logs/provider.go` implementing the Provider interface. Name: "logs", ShortDesc: "Query Loki datasources and manage Adaptive Logs". Commands() returns a single `logs` root command with:
- `query` — reuses LokiCmd constructor from T1
- `labels` — reuses Loki labels constructor from T1
- `series` — reuses Loki series constructor from T1
- `adaptive` — parent command containing the adaptive logs subtree (reuses `internal/providers/adaptive/logs.Commands()`)

ConfigKeys returns `[]ConfigKey{{Name: "logs-tenant-id"}, {Name: "logs-tenant-url"}}`.
TypedRegistrations returns the exemption and segment registrations currently in adaptive provider.
Validate returns nil.

**Deliverables:**
- `internal/providers/logs/provider.go`

**Acceptance criteria:**
- GIVEN `gcx logs query --help` WHEN executed THEN help text shows query subcommand with `gcx logs query` examples (FR-002, FR-027)
- GIVEN `gcx logs labels -d <uid>` WHEN executed THEN output is identical to former `gcx datasources loki labels -d <uid>` (FR-012)
- GIVEN `gcx logs adaptive exemptions list` WHEN executed THEN output is identical to former `gcx adaptive logs exemptions list` (FR-018)
- GIVEN `gcx providers list` WHEN executed THEN "logs" appears in the list (FR-029)

---

### T5: Implement traces provider
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `internal/providers/traces/provider.go` implementing the Provider interface. Name: "traces", ShortDesc: "Query Tempo datasources and manage Adaptive Traces". Commands() returns a single `traces` root command with:
- `query` — reuses TempoCmd constructor from T1
- `adaptive` — parent command containing the adaptive traces subtree (reuses `internal/providers/adaptive/traces.Commands()`)

ConfigKeys returns `[]ConfigKey{{Name: "traces-tenant-id"}, {Name: "traces-tenant-url"}}`.
TypedRegistrations returns the policy registration currently in adaptive provider.
Validate returns nil.

**Deliverables:**
- `internal/providers/traces/provider.go`

**Acceptance criteria:**
- GIVEN `gcx traces query --help` WHEN executed THEN help text shows query subcommand with `gcx traces query` examples (FR-003, FR-027)
- GIVEN `gcx traces adaptive policies list` WHEN executed THEN output is identical to former `gcx adaptive traces policies list` (FR-019)
- GIVEN `gcx providers list` WHEN executed THEN "traces" appears in the list (FR-029)

---

### T6: Implement profiles provider
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `internal/providers/profiles/provider.go` implementing the Provider interface. Name: "profiles", ShortDesc: "Query Pyroscope datasources and manage continuous profiling". Commands() returns a single `profiles` root command with:
- `query` — reuses PyroscopeCmd constructor from T1
- `labels` — reuses Pyroscope labels constructor from T1
- `profile-types` — reuses Pyroscope profile-types constructor from T1
- `series` — reuses PyroscopeSeriesCmd constructor from T1
- `adaptive` — stub command that prints "Adaptive Profiles is not yet available." to stderr and returns nil

ConfigKeys returns nil (no adaptive config needed yet).
TypedRegistrations returns nil.
Validate returns nil.

**Deliverables:**
- `internal/providers/profiles/provider.go`

**Acceptance criteria:**
- GIVEN `gcx profiles query --help` WHEN executed THEN help text shows query subcommand with `gcx profiles query` examples (FR-004, FR-027)
- GIVEN `gcx profiles labels -d <uid>` WHEN executed THEN output is identical to former `gcx datasources pyroscope labels -d <uid>` (FR-014)
- GIVEN `gcx profiles adaptive` WHEN executed THEN "Adaptive Profiles is not yet available." is printed to stderr and exit code is 0 (FR-020, NC-008)
- GIVEN `gcx profiles adaptive` WHEN stdout is captured THEN stdout is empty (NC-008)
- GIVEN `gcx providers list` WHEN executed THEN "profiles" appears in the list (FR-029)

---

## Wave 3: Cleanup

### T7: Remove old commands and adaptive provider
**Priority**: P0
**Effort**: Medium
**Depends on**: T2, T3, T4, T5, T6
**Type**: task

Remove the LGTM-specific subcommands from the datasources command tree:
- Remove `prometheusCmd`, `lokiCmd`, `pyroscopeCmd`, `tempoCmd` calls from `cmd/gcx/datasources/command.go`
- Delete or strip the parent command files (`cmd/gcx/datasources/prometheus.go`, `loki.go`, `pyroscope.go`, `tempo.go`) — keep any exported constructors needed by signal providers

Delete the unified adaptive provider:
- Delete `internal/providers/adaptive/provider.go` and `internal/providers/adaptive/provider_test.go`
- Remove the blank import `_ "github.com/grafana/gcx/internal/providers/adaptive"` from `cmd/gcx/root/command.go`
- Add blank imports for the four new signal providers: `_ "github.com/grafana/gcx/internal/providers/metrics"`, `logs`, `traces`, `profiles`

The adaptive signal subpackages (`internal/providers/adaptive/metrics/`, `logs/`, `traces/`) and `internal/providers/adaptive/auth/` MUST remain — they are imported by the new signal providers.

**Deliverables:**
- `cmd/gcx/datasources/command.go` — remove LGTM subcommand additions
- `cmd/gcx/datasources/prometheus.go` — delete or strip to exports-only
- `cmd/gcx/datasources/loki.go` — delete or strip to exports-only
- `cmd/gcx/datasources/pyroscope.go` — delete or strip to exports-only
- `cmd/gcx/datasources/tempo.go` — delete
- `internal/providers/adaptive/provider.go` — delete
- `internal/providers/adaptive/provider_test.go` — delete
- `cmd/gcx/root/command.go` — update blank imports

**Acceptance criteria:**
- GIVEN `gcx datasources --help` WHEN executed THEN only `list`, `get`, and `query` subcommands are shown (FR-021, FR-028)
- GIVEN `gcx datasources prometheus` WHEN executed THEN command is not found (NC-002)
- GIVEN `gcx adaptive` WHEN executed THEN command is not found (FR-023, NC-002)
- GIVEN `go build ./...` and `go test ./...` WHEN run THEN both pass with zero errors
- GIVEN all signal commands WHEN executed THEN they still work correctly (no regressions)

---

## Wave 4: Agent Annotations and Documentation

### T8: Agent mode annotations and CLI reference docs
**Priority**: P1
**Effort**: Medium
**Depends on**: T7
**Type**: chore

Add agent mode annotations for all new signal provider commands in `internal/agent/known_resources.go`. Each signal's query, labels, metadata, targets, series, profile-types, and adaptive subcommands need OperationHint entries with appropriate token cost estimates and example commands.

Remove agent annotations for deleted commands (adaptive metrics/logs/traces, datasources prometheus/loki/tempo/pyroscope).

Regenerate CLI reference documentation by running `GCX_AGENT_MODE=false make docs`. Update any manually-maintained doc files that reference old command paths.

Update `CLAUDE.md` package map to reflect the new provider packages under `internal/providers/`.

**Deliverables:**
- `internal/agent/known_resources.go` — new entries for signal commands, remove old entries (FR-026)
- `docs/reference/cli/` — regenerated CLI docs
- `CLAUDE.md` — updated package map

**Acceptance criteria:**
- GIVEN agent mode is enabled WHEN `gcx commands` is executed THEN all signal provider commands appear with correct OperationHint metadata (FR-026)
- GIVEN `GCX_AGENT_MODE=false make all` WHEN executed THEN build, lint, test, and docs all pass with zero errors
- GIVEN the CLAUDE.md package map WHEN reviewed THEN it lists `metrics/`, `logs/`, `traces/`, `profiles/` under `internal/providers/`
