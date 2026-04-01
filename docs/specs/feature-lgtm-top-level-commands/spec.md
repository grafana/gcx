---
type: feature-spec
title: "Top-Level LGTM Signal Commands"
status: done
beads_id: 309
created: 2026-04-01
---

# Top-Level LGTM Signal Commands

## Problem Statement

Grafana's native LGTM databases (Mimir/Prometheus, Loki, Tempo, Pyroscope) are buried under `gcx datasources <type> query`, requiring unnecessary navigation depth. The previous version of gcx exposed these as top-level commands (`gcx traces search`, etc.), and team members have flagged the current nesting as confusing because native LGTM databases are products in their own right, not merely datasources.

Additionally, `gcx datasources generic` is a misleading name for the auto-detecting query subcommand -- users expect "generic" to mean something different from "auto-detect the datasource type and query it."

The adaptive telemetry provider (`gcx adaptive metrics|logs|traces`) is currently a standalone top-level command, but its subcommands naturally belong under the per-signal product commands (`gcx metrics adaptive ...`). The adaptive provider and its single-provider registration should be replaced by four per-signal providers that each own their complete command surface.

Current workaround: Users must memorize that `gcx datasources prometheus query` is the path for metrics queries, and separately that `gcx adaptive metrics rules show` is the path for adaptive metrics management -- two unrelated command trees for the same signal type.

## Scope

### In Scope

- Four new signal providers registered via `providers.Register`: `metrics`, `logs`, `traces`, `profiles`
- Each signal provider's `Commands()` returns a single root command (e.g., `metrics`) containing both datasource-origin subcommands (query, labels, metadata, targets, series, profile-types) AND adaptive subcommands
- Deletion of the unified adaptive provider (`internal/providers/adaptive/provider.go`)
- Removal of `gcx datasources prometheus`, `gcx datasources loki`, `gcx datasources tempo`, `gcx datasources pyroscope` from the datasources command tree
- Removal of the `gcx adaptive` top-level command
- Renaming `gcx datasources generic` to `gcx datasources query` (old `generic` subcommand removed, no alias)
- A `gcx profiles adaptive` stub command that prints a message indicating adaptive profiles is not yet available
- Relocation of adaptive signal subpackages (`internal/providers/adaptive/metrics/`, `logs/`, `traces/`, `auth/`) into the new per-signal provider packages
- Agent mode annotations (OperationHint metadata) for all new commands
- Updated command examples in help text to reflect new paths
- Updated CLI reference docs (`docs/reference/cli/`)

### Out of Scope

- **Backward-compatible aliases or deprecation warnings** -- gcx is pre-GA; old paths are removed with no shim
- **New query functionality for Tempo** -- Tempo query remains unchanged; this spec only moves it to its new location
- **Non-LGTM datasource changes** -- commands like `gcx datasources list` and `gcx datasources get` stay where they are
- **Adaptive profiles backend** -- `gcx profiles adaptive` is a stub only; implementing an actual adaptive profiles provider is a separate feature
- **Config schema changes** -- existing config keys (`default-prometheus-datasource`, `default-loki-datasource`, `metrics-tenant-id`, `logs-tenant-id`, `traces-tenant-id`, etc.) remain unchanged
- **Provider interface changes** -- the `providers.Provider` interface is unchanged

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Signal naming vs. database naming | `metrics` (not `prometheus`), `logs` (not `loki`), `traces` (not `tempo`), `profiles` (not `pyroscope`) | Signal names are product-level concepts; database names remain at the datasource level for non-native use | Issue |
| Adaptive nesting location | Under each signal command (`gcx metrics adaptive ...`) | Groups all operations for a signal type together instead of spreading across two command trees | Issue |
| Backward compatibility approach | No deprecation period; old paths removed entirely | gcx is pre-GA -- clean break is preferred over carrying compatibility debt | User feedback |
| 4 providers vs. 1 umbrella | Each signal gets its own provider implementing `providers.Provider` | Each provider self-registers, appears in `gcx providers list`, owns its config keys, and returns one root command containing both datasource-origin and adaptive subcommands | User feedback + codebase analysis |
| Adaptive provider disposition | Delete entirely; split into per-signal providers | The adaptive subpackages (`metrics/`, `logs/`, `traces/`) are already isolated; the umbrella `provider.go` adds no value when signals are top-level | User feedback + codebase analysis |
| Command constructor reuse | New providers import existing constructors (`query.PrometheusCmd`, `labelsCmd`, etc.) | Zero code duplication; single source of truth for query/labels/metadata logic | Codebase (existing shared constructors) |
| Profiles adaptive stub | `gcx profiles adaptive` exists as a stub printing "not yet available" | Provides a consistent command surface; users discover the planned feature | User feedback |
| Generic rename | `gcx datasources generic` removed; `gcx datasources query` takes its place with no alias | Pre-GA allows clean rename without backward compat | User feedback |

## Functional Requirements

**FR-001:** The system MUST register a `metrics` provider via `providers.Register` whose `Commands()` returns a root `metrics` command containing: `query`, `labels`, `metadata`, `targets`, and `adaptive` subcommands.

**FR-002:** The system MUST register a `logs` provider via `providers.Register` whose `Commands()` returns a root `logs` command containing: `query`, `labels`, `series`, and `adaptive` subcommands.

**FR-003:** The system MUST register a `traces` provider via `providers.Register` whose `Commands()` returns a root `traces` command containing: `query` and `adaptive` subcommands.

**FR-004:** The system MUST register a `profiles` provider via `providers.Register` whose `Commands()` returns a root `profiles` command containing: `query`, `labels`, `profile-types`, `series`, and `adaptive` subcommands.

**FR-005:** The `gcx metrics query` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources prometheus query`.

**FR-006:** The `gcx logs query` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources loki query`.

**FR-007:** The `gcx traces query` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources tempo query`.

**FR-008:** The `gcx profiles query` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources pyroscope query`.

**FR-009:** The `gcx metrics labels` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources prometheus labels`.

**FR-010:** The `gcx metrics metadata` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources prometheus metadata`.

**FR-011:** The `gcx metrics targets` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources prometheus targets`.

**FR-012:** The `gcx logs labels` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources loki labels`.

**FR-013:** The `gcx logs series` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources loki series`.

**FR-014:** The `gcx profiles labels` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources pyroscope labels`.

**FR-015:** The `gcx profiles profile-types` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources pyroscope profile-types`.

**FR-016:** The `gcx profiles series` command MUST accept the same flags and arguments and produce the same output as the current `gcx datasources pyroscope series`.

**FR-017:** The `gcx metrics adaptive` subcommand tree MUST contain the same commands currently under `gcx adaptive metrics` (`rules show`, `rules sync`, `recommendations show`, `recommendations apply`).

**FR-018:** The `gcx logs adaptive` subcommand tree MUST contain the same commands currently under `gcx adaptive logs` (`patterns show`, `patterns stats`, `exemptions list|create|update|delete`, `segments list|create|update|delete`).

**FR-019:** The `gcx traces adaptive` subcommand tree MUST contain the same commands currently under `gcx adaptive traces` (`policies list|get|create|update|delete`, `recommendations show|apply|dismiss`).

**FR-020:** The `gcx profiles adaptive` command MUST exist and MUST print a message to stderr indicating that adaptive profiles is not yet available, then exit with code 0.

**FR-021:** The `gcx datasources` command tree MUST NOT contain `prometheus`, `loki`, `tempo`, `pyroscope`, or `generic` subcommands. These MUST be removed entirely.

**FR-022:** The `gcx datasources query` command MUST replace the current `gcx datasources generic query`, accepting the same flags and arguments and producing the same output.

**FR-023:** The `gcx adaptive` top-level command MUST be removed entirely.

**FR-024:** Each of the four signal providers MUST return appropriate `ConfigKeys()`: the `metrics` provider MUST return `metrics-tenant-id` and `metrics-tenant-url`; the `logs` provider MUST return `logs-tenant-id` and `logs-tenant-url`; the `traces` provider MUST return `traces-tenant-id` and `traces-tenant-url`. The `profiles` provider MUST return an empty config key list (no adaptive config yet).

**FR-025:** Each of the four signal providers MUST return appropriate `TypedRegistrations()`: the `logs` provider MUST return the exemption and segment adapter registrations currently in the adaptive provider; the `traces` provider MUST return the policy adapter registration currently in the adaptive provider. The `metrics` and `profiles` providers MUST return nil.

**FR-026:** All new top-level signal commands and their subcommands MUST include agent mode annotations (OperationHint metadata) with appropriate token cost and LLM hints.

**FR-027:** Help text examples for all commands MUST use the new command paths (e.g., `gcx metrics query` not `gcx datasources prometheus query`).

**FR-028:** The `gcx datasources` command MUST retain `list`, `get`, and the new `query` subcommands. Its help text MUST be updated to reflect only these remaining subcommands.

**FR-029:** Each signal provider MUST appear in the output of `gcx providers list` (or equivalent provider enumeration).

## Acceptance Criteria

- GIVEN a user with a configured Grafana context and a Prometheus datasource
  WHEN they run `gcx metrics query <datasource-uid> '<promql-expr>'`
  THEN the query executes and returns results identical to what `gcx datasources prometheus query` previously returned

- GIVEN a user with a configured Grafana context and a Loki datasource
  WHEN they run `gcx logs query <datasource-uid> '<logql-expr>'`
  THEN the query executes and returns results identical to what `gcx datasources loki query` previously returned

- GIVEN a user with a configured Grafana context and a Tempo datasource
  WHEN they run `gcx traces query`
  THEN the command behaves identically to what `gcx datasources tempo query` previously did

- GIVEN a user with a configured Grafana context and a Pyroscope datasource
  WHEN they run `gcx profiles query <datasource-uid> '<expr>'`
  THEN the query executes and returns results identical to what `gcx datasources pyroscope query` previously returned

- GIVEN a user
  WHEN they run `gcx datasources query <datasource-uid> '<expr>'`
  THEN the auto-detecting query executes identically to what `gcx datasources generic query` previously did

- GIVEN a user
  WHEN they run `gcx datasources prometheus` (or `loki`, `tempo`, `pyroscope`, `generic`)
  THEN the CLI returns an "unknown command" error

- GIVEN a user
  WHEN they run `gcx adaptive`
  THEN the CLI returns an "unknown command" error

- GIVEN a user with adaptive metrics configured
  WHEN they run `gcx metrics adaptive rules show`
  THEN the aggregation rules are displayed identically to what `gcx adaptive metrics rules show` previously returned

- GIVEN a user with adaptive logs configured
  WHEN they run `gcx logs adaptive` with any valid subcommand (e.g., `patterns show`, `exemptions list`)
  THEN the command produces the same result as the equivalent former `gcx adaptive logs` subcommand

- GIVEN a user with adaptive traces configured
  WHEN they run `gcx traces adaptive` with any valid subcommand (e.g., `policies list`, `recommendations show`)
  THEN the command produces the same result as the equivalent former `gcx adaptive traces` subcommand

- GIVEN a user
  WHEN they run `gcx profiles adaptive`
  THEN a message is printed to stderr indicating adaptive profiles is not yet available AND the exit code is 0

- GIVEN a user
  WHEN they run `gcx metrics labels -d <uid>`
  THEN the Prometheus labels are returned identically to what `gcx datasources prometheus labels -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx metrics metadata -d <uid>`
  THEN the Prometheus metadata is returned identically to what `gcx datasources prometheus metadata -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx metrics targets -d <uid>`
  THEN the Prometheus targets are returned identically to what `gcx datasources prometheus targets -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx logs labels -d <uid>`
  THEN the Loki labels are returned identically to what `gcx datasources loki labels -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx logs series -d <uid> --match '{job="app"}'`
  THEN the Loki series are returned identically to what `gcx datasources loki series -d <uid> --match '{job="app"}'` previously returned

- GIVEN a user
  WHEN they run `gcx profiles labels -d <uid>`
  THEN the Pyroscope labels are returned identically to what `gcx datasources pyroscope labels -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx profiles profile-types -d <uid>`
  THEN the Pyroscope profile types are returned identically to what `gcx datasources pyroscope profile-types -d <uid>` previously returned

- GIVEN a user
  WHEN they run `gcx profiles series -d <uid> --match '{...}'`
  THEN the Pyroscope series are returned identically to what `gcx datasources pyroscope series -d <uid> --match '{...}'` previously returned

- GIVEN a user
  WHEN they run `gcx --help`
  THEN `metrics`, `logs`, `traces`, and `profiles` appear as top-level commands AND `adaptive` does NOT appear as a separate top-level entry AND the deprecated datasource subcommands (`prometheus`, `loki`, `tempo`, `pyroscope`, `generic`) do NOT appear under `gcx datasources`

- GIVEN a user
  WHEN they run `gcx providers list`
  THEN `metrics`, `logs`, `traces`, and `profiles` appear in the output AND `adaptive` does NOT appear

- GIVEN an agent interacting with gcx in agent mode
  WHEN it queries `gcx commands` or inspects command annotations
  THEN the new signal commands have OperationHint metadata with appropriate token cost and LLM hints AND no annotations exist for removed commands

## Negative Constraints

- **NC-001:** The implementation MUST NOT duplicate business logic (query execution, client creation, response formatting). New signal provider commands MUST delegate to the same constructor functions used by the former datasource commands.

- **NC-002:** The implementation MUST NOT retain old command paths as aliases, deprecated wrappers, or hidden commands. Old paths (`gcx datasources prometheus`, `gcx datasources loki`, `gcx datasources tempo`, `gcx datasources pyroscope`, `gcx datasources generic`, `gcx adaptive`) MUST be removed entirely.

- **NC-003:** The implementation MUST NOT change the output format, exit codes, or error messages of any command when invoked via its new path. Command behavior MUST be identical to the former path.

- **NC-004:** The implementation MUST NOT modify the `providers.Provider` interface or the self-registration pattern (`providers.Register`).

- **NC-005:** The implementation MUST NOT change config key names or default datasource UID resolution behavior. `default-prometheus-datasource` MUST continue to work for `gcx metrics query` exactly as it did for `gcx datasources prometheus query`.

- **NC-006:** The `gcx datasources list` and `gcx datasources get` commands MUST NOT be affected by this change.

- **NC-007:** The adaptive auth package (`internal/providers/adaptive/auth/`) MUST remain a shared utility importable by all signal providers. It MUST NOT be duplicated per provider.

- **NC-008:** The `profiles adaptive` stub MUST NOT print to stdout. The informational message MUST go to stderr only.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Cobra flag conflicts when mounting both datasource-origin and adaptive subcommands under the same parent | Runtime panic or flag registration errors | Each subcommand tree uses its own `*cobra.Command` instances; persistent flags from `ConfigLoader.BindFlags` are bound on the signal root command's persistent flags, shared by all children |
| Breaking external scripts or CI pipelines that reference old command paths | Scripts fail after upgrade | gcx is pre-GA; document breaking changes in release notes; provide migration guide mapping old paths to new paths |
| Splitting the adaptive provider into 4 providers changes the output of `gcx providers list` | Users/agents expecting "adaptive" provider find 4 signal providers instead | Document in release notes; new provider names are more intuitive than "adaptive" |
| Adapter registrations (logs exemptions/segments, traces policies) must move to new providers | Missing registrations cause `gcx get`/`gcx push` failures for adaptive resource types | Test that `adapter.All()` includes all former adaptive registrations after the split |
| Shared adaptive auth package import path changes if relocated | Compilation errors across providers | Keep `internal/providers/adaptive/auth/` at its current path OR relocate once and update all imports atomically |
| `gcx profiles adaptive` stub creates user expectation for a feature that does not exist | Support requests about non-functional feature | Stub message MUST clearly state "not yet available" with no timeline; `--help` text reinforces this |

## Open Questions

- [RESOLVED] Whether to use signal names (`metrics`) or database names (`prometheus`) at the top level -- **Decision: signal names at top level, database names at datasource level.**

- [RESOLVED] Whether to nest adaptive under signal commands or keep it standalone -- **Decision: nest under signal commands (`gcx metrics adaptive ...`).**

- [RESOLVED] Whether to keep old paths with deprecation warnings -- **Decision: No. Pre-GA clean break; old paths removed entirely.**

- [RESOLVED] Whether to keep the unified adaptive provider or split into per-signal providers -- **Decision: Split into 4 signal providers; delete the unified adaptive provider.**

- [RESOLVED] Whether `gcx profiles adaptive` should exist as a stub -- **Decision: Yes, stub that prints "not yet available" to stderr.**

- [RESOLVED] Whether `gcx datasources generic` should keep a deprecated alias -- **Decision: No alias. Renamed to `gcx datasources query` with clean break.**
