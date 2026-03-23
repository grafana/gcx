---
type: feature-tasks
title: "Move query under datasources with per-kind subcommands"
status: draft
spec: spec/feature-query-refactor/spec.md
plan: spec/feature-query-refactor/plan.md
created: 2026-03-18
---

# Implementation Tasks

## Dependency Graph

```
T1 (config: Datasources field + shared resolver)
├──→ T2 (query subcommand package: group + typed subcommands)
├──→ T3 (migrate existing datasource commands to shared resolver)
└──→ T4 (migrate synth provider to shared resolver)

T2 ──→ T5 (wire into datasources, remove top-level query, update docs)
T3 ──→ T5
T4 ──→ T5

T5 ──→ T6 (cleanup: delete old query package)
```

## Wave 1: Config Foundation

### T1: Add Datasources config section and shared resolver

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Add the `Datasources map[string]string` field to the `Context` struct in `internal/config/types.go`. Create `internal/config/resolver.go` with a `DefaultDatasourceUID(ctx Context, kind string) string` function that implements the 2-tier precedence: (1) `ctx.Datasources[kind]`, (2) legacy flat field for the kind. Add unit tests in `internal/config/resolver_test.go` covering all precedence scenarios. The config editor already supports map traversal via reflection, so `config set`/`unset` for `contexts.X.datasources.Y` will work without editor changes — verify this with a test.

**Deliverables:**
- `internal/config/types.go` — add `Datasources` field to `Context`
- `internal/config/resolver.go` — `DefaultDatasourceUID` function
- `internal/config/resolver_test.go` — unit tests

**Acceptance criteria:**
- GIVEN a Context with `Datasources["prometheus"]` set to `"new-uid"` and `DefaultPrometheusDatasource` set to `"legacy-uid"`
  WHEN `DefaultDatasourceUID(ctx, "prometheus")` is called
  THEN it returns `"new-uid"` (new key takes precedence per FR-022)

- GIVEN a Context with only `DefaultPrometheusDatasource` set to `"legacy-uid"` and no `Datasources` entry
  WHEN `DefaultDatasourceUID(ctx, "prometheus")` is called
  THEN it returns `"legacy-uid"` (legacy fallback per FR-023)

- GIVEN a Context with neither `Datasources["loki"]` nor `DefaultLokiDatasource` set
  WHEN `DefaultDatasourceUID(ctx, "loki")` is called
  THEN it returns `""` (empty string, caller handles error)

- GIVEN a fresh config
  WHEN a user runs `grafanactl config set contexts.myctx.datasources.prometheus prom-uid-123`
  THEN the config file contains a `datasources` section under `myctx` with `prometheus: prom-uid-123` (FR-021)

- GIVEN a config with `datasources.loki` set
  WHEN a user runs `grafanactl config unset contexts.myctx.datasources.loki`
  THEN the `loki` key is removed from the `datasources` section (FR-021)

---

## Wave 2: New Query Subcommands + Migration

### T2: Create datasources query subcommand package

**Priority**: P0
**Effort**: Large
**Depends on**: T1
**Type**: task

Create the `cmd/grafanactl/datasources/query/` package with the full command hierarchy. Implement the `query` group command (FR-001, FR-014), and all five subcommands: `prometheus` (FR-003, FR-004, FR-006, FR-007, FR-008, FR-010), `loki` (FR-003, FR-004, FR-006, FR-007, FR-008, FR-010, FR-019), `pyroscope` (FR-003, FR-004, FR-006, FR-007, FR-008, FR-009, FR-010), `tempo` (FR-018), and `generic` (FR-003, FR-005, FR-012, FR-013, FR-017). Move `ParseTime`, `ParseDuration` from `cmd/grafanactl/query/time.go` and the codecs from `command.go`/`graph.go` into the new package. Each typed subcommand uses positional args (`DATASOURCE_UID EXPR`), calls the shared resolver from T1 for default UID resolution, validates datasource type via API, and delegates to the existing query client. Implement `--window` mutual exclusion with `--from`/`--to` (FR-008). Implement `--limit` on `loki` and `generic` (FR-019, FR-013). Add unit tests for flag validation, window/from/to exclusion, and arg parsing.

**Deliverables:**
- `cmd/grafanactl/datasources/query/command.go` — group command + shared helpers
- `cmd/grafanactl/datasources/query/prometheus.go` — prometheus subcommand
- `cmd/grafanactl/datasources/query/loki.go` — loki subcommand
- `cmd/grafanactl/datasources/query/pyroscope.go` — pyroscope subcommand
- `cmd/grafanactl/datasources/query/tempo.go` — tempo stub
- `cmd/grafanactl/datasources/query/generic.go` — generic auto-detect
- `cmd/grafanactl/datasources/query/codecs.go` — table/wide/graph codecs
- `cmd/grafanactl/datasources/query/time.go` — time parsing (moved)
- `cmd/grafanactl/datasources/query/time_test.go` — time tests (moved)

**Acceptance criteria:**
- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query --help`
  THEN the output lists `prometheus`, `loki`, `pyroscope`, `tempo`, and `generic` as available subcommands (FR-002)

- GIVEN a valid Prometheus datasource UID `abc123`
  WHEN a user runs `grafanactl datasources query prometheus abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m`
  THEN the command executes a range query and prints results in table format (FR-003, FR-007)

- GIVEN a valid Prometheus datasource UID `abc123`
  WHEN a user runs `grafanactl datasources query prometheus abc123 'up' --window 1h`
  THEN the command executes a range query with `from=now-1h` and `to=now` (FR-008)

- GIVEN a user provides both `--window` and `--from`
  WHEN the command is invoked
  THEN the command returns an error stating that `--window` is mutually exclusive with `--from` and `--to` (FR-008)

- GIVEN a datasource UID `prom-001` that is type `prometheus`
  WHEN a user runs `grafanactl datasources query loki prom-001 '{job="x"}'`
  THEN the command returns an error containing "datasource prom-001 is type prometheus, not loki" (FR-006)

- GIVEN a Loki datasource UID `loki-001`
  WHEN a user runs `grafanactl datasources query loki loki-001 '{job="varlogs"}' --from now-1h --to now --limit 500`
  THEN the command executes a Loki log query with a limit of 500 (FR-019)

- GIVEN a user runs `grafanactl datasources query prometheus`
  WHEN `--profile-type` is passed as a flag
  THEN the command returns an "unknown flag" error (FR-009 negative constraint)

- GIVEN a user runs `grafanactl datasources query prometheus`
  WHEN `--limit` is passed as a flag
  THEN the command returns an "unknown flag" error (FR-019 scoping)

- GIVEN a Pyroscope datasource UID `pyro-001`
  WHEN a user runs `grafanactl datasources query pyroscope pyro-001 '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now`
  THEN the command executes a Pyroscope query (FR-009)

- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query tempo`
  THEN the command returns an error with message "tempo queries are not yet implemented" and exits with code 1 (FR-018)

- GIVEN a datasource UID `any-001` of any supported type
  WHEN a user runs `grafanactl datasources query generic any-001 'some_expr' --from now-1h --to now`
  THEN the command auto-detects the datasource type and executes the appropriate query (FR-005, FR-012)

- GIVEN the `generic` subcommand
  WHEN a user runs `grafanactl datasources query generic` without a UID argument
  THEN the command returns an error requiring `DATASOURCE_UID` (FR-017)

---

### T3: Migrate existing datasource commands to shared resolver

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Replace all inline default datasource UID resolution in `cmd/grafanactl/datasources/prometheus.go` (3 commands: labels, metadata, targets), `cmd/grafanactl/datasources/loki.go` (2 commands: labels, series), and `cmd/grafanactl/datasources/pyroscope.go` (2 commands: profile-types, labels) with calls to `config.DefaultDatasourceUID(ctx, kind)` from T1. The observable behavior of these commands MUST NOT change — they gain support for the new `datasources` config section transparently.

**Deliverables:**
- `cmd/grafanactl/datasources/prometheus.go` — updated resolution in labels, metadata, targets commands
- `cmd/grafanactl/datasources/loki.go` — updated resolution in labels, series commands
- `cmd/grafanactl/datasources/pyroscope.go` — updated resolution in profile-types, labels commands

**Acceptance criteria:**
- GIVEN a Grafana context with `datasources.prometheus` configured
  WHEN a user runs `grafanactl datasources prometheus labels` (existing command)
  THEN the command uses the UID from `datasources.prometheus` via the shared resolver (FR-024)

- GIVEN a Grafana context with only `default-loki-datasource` configured (no new section)
  WHEN a user runs `grafanactl datasources loki labels`
  THEN the command uses the legacy UID (FR-024 fallback behavior unchanged)

- GIVEN a Grafana context with `datasources.pyroscope` configured
  WHEN a user runs `grafanactl datasources pyroscope profile-types`
  THEN the command uses the UID from `datasources.pyroscope` via the shared resolver (FR-024)

---

### T4: Migrate synth provider to shared resolver

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Update the datasource UID resolution in `internal/providers/synth/checks/status.go` to call `config.DefaultDatasourceUID(ctx, "prometheus")` for tiers 1-2 (new config section + legacy key). The synth provider's own tiers 3-4 (provider cache lookup + SM API auto-discovery) remain unchanged and execute only when the shared resolver returns empty. The 4-tier resolution order becomes: (1) `datasources.prometheus`, (2) `default-prometheus-datasource`, (3) `providers.synth.sm-metrics-datasource-uid`, (4) auto-discover via SM plugin settings.

**Deliverables:**
- `internal/providers/synth/checks/status.go` — updated resolution logic

**Acceptance criteria:**
- GIVEN a Grafana context with `datasources.prometheus` configured
  WHEN the synth provider resolves a default Prometheus datasource UID
  THEN it uses the UID from `datasources.prometheus` via the shared resolver (FR-023)

- GIVEN a Grafana context with neither `datasources.prometheus` nor `default-prometheus-datasource` but with synth provider cache
  WHEN the synth provider resolves a default Prometheus datasource UID
  THEN it falls back to the provider cache (tier 3, existing behavior preserved)

---

## Wave 3: Wiring and Cleanup

### T5: Wire query subcommand, remove top-level query, regenerate docs

**Priority**: P0
**Effort**: Medium
**Depends on**: T2, T3, T4
**Type**: task

Wire the new `query` group command into `cmd/grafanactl/datasources/command.go` via `cmd.AddCommand(query.Command())`. Remove the `rootCmd.AddCommand(query.Command())` line and the `query` import from `cmd/grafanactl/root/command.go` (FR-011). Run `GRAFANACTL_AGENT_MODE=false make all` to regenerate CLI docs and verify the build passes lint, tests, and doc generation (FR-016). Verify that the generated docs under `docs/reference/cli/` reflect the new hierarchy and do not contain a top-level `query` reference.

**Deliverables:**
- `cmd/grafanactl/datasources/command.go` — add query subcommand import + registration
- `cmd/grafanactl/root/command.go` — remove query import + registration
- `docs/reference/cli/` — regenerated docs (via `make docs`)

**Acceptance criteria:**
- GIVEN the CLI is built
  WHEN a user runs `grafanactl query`
  THEN the command returns an "unknown command" error (FR-011)

- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query --help`
  THEN the output lists `prometheus`, `loki`, `pyroscope`, `tempo`, and `generic` as subcommands (FR-001, FR-002)

- GIVEN the codebase after this change
  WHEN `GRAFANACTL_AGENT_MODE=false make all` is run
  THEN lint passes, all tests pass, and the generated CLI reference docs reflect `datasources query {prometheus,loki,pyroscope,tempo,generic}` and do NOT contain a top-level `query` command (FR-016)

---

### T6: Delete old query package

**Priority**: P2
**Effort**: Small
**Depends on**: T5
**Type**: chore

Delete the now-unused `cmd/grafanactl/query/` package entirely (`command.go`, `graph.go`, `time.go`, `time_test.go`). Verify that `make all` still passes after deletion — no other package should import from this path after T5.

**Deliverables:**
- Delete `cmd/grafanactl/query/command.go`
- Delete `cmd/grafanactl/query/graph.go`
- Delete `cmd/grafanactl/query/time.go`
- Delete `cmd/grafanactl/query/time_test.go`

**Acceptance criteria:**
- GIVEN the old `cmd/grafanactl/query/` package is deleted
  WHEN `GRAFANACTL_AGENT_MODE=false make all` is run
  THEN the build, lint, tests, and doc generation all pass with zero references to the deleted package
