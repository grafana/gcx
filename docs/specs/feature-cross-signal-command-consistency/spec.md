---
type: feature-spec
title: "Cross-Signal Command Consistency"
status: done
beads_id: gcx-c183
created: 2026-04-03
---

# Cross-Signal Command Consistency

## Problem Statement

gcx has four signal providers (metrics, logs, profiles, traces) that grew organically, resulting in inconsistent command naming, datasource selection patterns, and visualization support. Users must learn two different datasource selection patterns (positional `[UID] EXPR` for query commands vs `-d` flag for discovery commands), identical command names have different semantics across signals (`profiles series` returns time series data while `logs series` returns stream metadata), and metric LogQL queries produce broken output because `logs query` forces numeric time series into the log stream format.

**Who is affected**: All gcx users interacting with signal providers, and AI agents that generate gcx commands based on pattern matching across signals.

**Current workarounds**: Users must memorize which pattern each command uses. Metric LogQL users must parse raw JSON output manually because `-o table` and `-o graph` produce garbled results. There is no workaround for the `profiles series` / `logs series` naming collision -- users must consult per-signal docs to know what each command returns.

## Scope

### In Scope

- Migrate all signal provider query commands from positional `[UID] EXPR` to `-d/--datasource` flag with config fallback
- Migrate `traces get` from positional `[UID] TRACE_ID` to `-d/--datasource TRACE_ID`
- Rename `profiles series` to `profiles metrics`
- Add new `logs metrics` command for metric LogQL queries with proper time-series formatters
- Merge `traces tags` and `traces tag-values` into `traces labels` with `-l/--label` flag
- Add non-deprecated aliases: `traces search` -> `traces query`, `traces tags` -> `traces labels`
- Drop `traces tag-values` command (no alias)
- Drop `--instant` flag from `traces metrics`; deduce instant vs range from time flag presence
- Add `--from`-without-`--to` and `--to`-without-`--from` validation to `SharedOpts.Validate()`
- Update all command `Use`, `Long`, `Example` strings and agent annotations (`AnnotationLLMHint`)

### Out of Scope

- **Changes to `gcx datasources query`**: This low-level escape hatch keeps its positional `DATASOURCE_UID EXPR` pattern because both args are always mandatory (no config default)
- **Deprecation machinery**: gcx is pre-GA; all changes are hard breaking with no shim or deprecation warning
- **New Loki client wire protocol**: `logs metrics` will use Loki's existing metric query API endpoint; no new Loki API features are needed
- **Changes to adaptive subcommands**: Adaptive Metrics, Adaptive Logs, and Adaptive Traces command structures are unchanged
- **CLI reference doc regeneration**: Docs auto-regenerate via `make docs` after code changes; no manual doc work is in scope
- **gcx skills / grafanactl plugin updates**: Downstream skill updates are follow-up work

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Datasource selection pattern | `-d/--datasource` flag everywhere (signal providers) | Uniform, self-documenting; eliminates ambiguity of two adjacent bare positional args; matches existing discovery command pattern | ADR: rejected positional `[UID] EXPR` and positionals-everywhere |
| Naming convention for time-series-over-time commands | `metrics` verb across logs/profiles/traces | Each backend's time-series query has a different response shape from its primary query; explicit commands enable purpose-built formatters | ADR: rejected single `query` with auto-detect, rejected `query-range` naming |
| `profiles series` rename | Rename to `profiles metrics`, no alias | Resolves collision with `logs series` (discovery); `metrics` aligns with cross-signal convention | ADR section 3 |
| Traces tag/label discovery | Merge into `labels` with `-l/--label` flag; `tags` as permanent alias | Matches metrics/logs/profiles pattern; `tags` alias preserved because Tempo users think in "tags" | ADR section 5 |
| Instant vs range deduction | Drop `--instant` flag; deduce from time flag presence | Matches how `metrics query` works; reduces flag surface | ADR section 5 |
| Breaking change strategy | Hard breaks, no deprecation window | Pre-GA; no backwards compatibility contract; avoids indefinite compatibility machinery | ADR section 6 |
| `traces search` rename | Rename to `traces query`; keep `search` as permanent non-deprecated alias | `query` is the canonical verb; `search` is natural Tempo UX | ADR section 6 |

## Functional Requirements

- FR-001: All signal provider commands (metrics, logs, profiles, traces) MUST accept datasource UID exclusively via the `-d/--datasource` flag. No signal provider command SHALL accept datasource UID as a positional argument.

- FR-002: When `-d/--datasource` is omitted, the command MUST resolve the datasource UID from the current context's `datasources.<type>` config key (e.g., `datasources.tempo`, `datasources.loki`, `datasources.prometheus`, `datasources.pyroscope`).

- FR-003: When `-d/--datasource` is omitted and no config default exists, the command MUST return an error: `"datasource UID is required: use -d flag or set datasources.<type> in config"`.

- FR-004: The expression (or trace ID for `traces get`) MUST be the sole positional argument for all signal provider query/metrics/get commands.

- FR-005: `gcx datasources query` MUST retain its positional `DATASOURCE_UID EXPR` pattern unchanged.

- FR-006: `profiles series` MUST be renamed to `profiles metrics`. The old `profiles series` command MUST NOT exist (no alias).

- FR-007: A new `logs metrics EXPR` command MUST be added that executes metric LogQL queries and returns time-series results with proper formatters (table, json, graph).

- FR-008: `logs metrics` MUST support `-o table`, `-o json`, `-o wide`, and `-o graph` output formats with time-series-appropriate rendering.

- FR-009: `logs query` MUST continue to handle only log-line LogQL queries. It MUST NOT attempt to format metric LogQL results.

- FR-010: `traces tags` and `traces tag-values` MUST be merged into a single `traces labels` command. When `-l/--label NAME` is provided, the command MUST return values for that label. When `-l` is omitted, the command MUST return all label names.

- FR-011: `traces labels` MUST retain the `--scope` and `-q/--query` flags from the current `tags`/`tag-values` commands.

- FR-012: `tags` MUST be registered as a non-deprecated alias for `traces labels`. `tags -l NAME` MUST behave identically to `labels -l NAME`.

- FR-013: `traces search` MUST be renamed to `traces query`. `search` MUST be registered as a non-deprecated alias for `traces query`.

- FR-014: The `--instant` flag MUST be removed from `traces metrics`. Instant vs range MUST be deduced from time flag presence using the rules in FR-016.

- FR-015: `SharedOpts.Validate()` MUST return an error when `--from` is provided without `--to` (message: `"--to is required when --from is set"`).

- FR-016: `SharedOpts.Validate()` MUST return an error when `--to` is provided without `--from` and without `--since` (message: `"--from is required when --to is set"`).

- FR-017: Time flag deduction rules MUST apply uniformly across all signal provider commands that use `SharedOpts`:
  - No time flags provided: instant query (current time)
  - `--since DURATION`: range query (resolved to `--from now-DURATION --to now`)
  - `--from X --to Y`: range query
  - `--from X` alone: error (FR-015)
  - `--to Y` alone: error (FR-016)
  - `--since` + `--from`: error (existing validation, mutually exclusive)

- FR-018: Every signal provider command MUST update its `Use`, `Long`, `Example` strings to reflect the `-d/--datasource` pattern and any renamed commands.

- FR-019: Every signal provider command MUST update its `agent.AnnotationLLMHint` annotation to use the `-d` flag pattern and current command names.

- FR-020: Signal-specific flags MUST remain unchanged: `--profile-type`, `--max-nodes`, `--top`, `--group-by`, `--aggregation` (profiles); `--scope`, `-q/--query` (traces labels); `--llm` (traces get); `--limit` (logs query, traces query); `-M/--match` (logs series); `-m/--metric` (metrics metadata); `-l/--label` (all labels commands).

## Acceptance Criteria

- GIVEN a user runs `gcx metrics query 'up'` with `datasources.prometheus` configured in context
  WHEN the command executes
  THEN it queries the configured default Prometheus datasource and returns results

- GIVEN a user runs `gcx metrics query -d prom-uid 'up'`
  WHEN the command executes
  THEN it queries the datasource with UID `prom-uid`

- GIVEN a user runs `gcx metrics query prom-uid 'up'` (positional UID, old pattern)
  WHEN the command parses arguments
  THEN it returns an error because `ExactArgs(1)` rejects two positional args

- GIVEN a user runs `gcx logs query '{job="varlogs"}'` with `datasources.loki` configured
  WHEN the command executes
  THEN it queries the default Loki datasource and returns log lines

- GIVEN a user runs `gcx traces query '{ status = error }' --since 1h`
  WHEN the command executes
  THEN it searches for traces matching the TraceQL expression in the last hour

- GIVEN a user runs `gcx traces search '{ status = error }'`
  WHEN the command executes
  THEN it behaves identically to `gcx traces query '{ status = error }'` (alias)

- GIVEN a user runs `gcx traces get TRACE_ID -d tempo-uid`
  WHEN the command executes
  THEN it retrieves the trace from datasource `tempo-uid`

- GIVEN a user runs `gcx traces get -d tempo-uid TRACE_ID`
  WHEN the command executes
  THEN it retrieves the trace (flag order MUST NOT matter)

- GIVEN a user runs `gcx profiles metrics '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h`
  WHEN the command executes
  THEN it returns time-series data (same behavior as current `profiles series`)

- GIVEN a user runs `gcx profiles series ...`
  WHEN the command parses
  THEN it returns an error: unknown command "series"

- GIVEN a user runs `gcx logs metrics 'rate({job="varlogs"}[5m])' --since 1h -o table`
  WHEN the command executes
  THEN it returns a table with time-series columns (timestamp, value, labels)

- GIVEN a user runs `gcx logs metrics 'rate({job="varlogs"}[5m])' --since 1h -o graph`
  WHEN the command executes
  THEN it renders a terminal line chart of the time-series data

- GIVEN a user runs `gcx traces labels -d tempo-uid`
  WHEN the command executes
  THEN it returns all tag names from the Tempo datasource

- GIVEN a user runs `gcx traces labels -l service.name -d tempo-uid`
  WHEN the command executes
  THEN it returns values for the `service.name` tag

- GIVEN a user runs `gcx traces tags -l service.name -d tempo-uid`
  WHEN the command executes
  THEN it behaves identically to `gcx traces labels -l service.name -d tempo-uid` (alias)

- GIVEN a user runs `gcx traces labels -l service.name --scope span -d tempo-uid`
  WHEN the command executes
  THEN it returns values filtered to the `span` scope

- GIVEN a user runs `gcx traces tag-values service.name -d tempo-uid`
  WHEN the command parses
  THEN it returns an error: unknown command "tag-values"

- GIVEN a user runs `gcx traces metrics '{ } | rate()' --since 1h`
  WHEN the command executes
  THEN it runs a range metrics query (no `--instant` flag needed)

- GIVEN a user runs `gcx traces metrics '{ } | rate()'` with no time flags
  WHEN the command executes
  THEN it runs an instant metrics query at the current time

- GIVEN a user runs `gcx traces metrics '{ } | rate()' --instant`
  WHEN the command parses flags
  THEN it returns an error: unknown flag "instant"

- GIVEN a user runs any signal command with `--from 2024-01-01T00:00:00Z` but no `--to`
  WHEN `SharedOpts.Validate()` runs
  THEN it returns the error `"--to is required when --from is set"`

- GIVEN a user runs any signal command with `--to 2024-01-01T00:00:00Z` but no `--from` and no `--since`
  WHEN `SharedOpts.Validate()` runs
  THEN it returns the error `"--from is required when --to is set"`

- GIVEN a user runs `gcx datasources query DATASOURCE_UID EXPR`
  WHEN the command executes
  THEN it works unchanged (positional UID pattern preserved)

- GIVEN no `-d` flag and no config default
  WHEN any signal provider command runs
  THEN it returns an error mentioning both the `-d` flag and the config key

## Negative Constraints

- NEVER accept datasource UID as a positional argument in any signal provider command (metrics, logs, profiles, traces). The sole exception is `gcx datasources query`.
- NEVER register `profiles series` as an alias for `profiles metrics`. The rename is a clean break.
- NEVER register `traces tag-values` as an alias for `traces labels`. The merge is a clean break.
- DO NOT add deprecation warnings, shim commands, or dual-form support for any removed/renamed commands.
- DO NOT change the argument parsing of `gcx datasources query`.
- NEVER allow `--from` without `--to` or `--to` without `--from`/`--since` to silently produce a zero-time fallback.
- DO NOT modify adaptive subcommand structures (adaptive metrics, adaptive logs, adaptive traces).
- DO NOT change signal-specific flags listed in FR-020.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking change disrupts existing users/scripts | Medium -- pre-GA, but internal Grafana teams may have scripts | Hard break is intentional; communicate via PR description and changelog |
| `logs metrics` Loki client path may not exist | High -- requires new HTTP client method for metric queries | Reference existing Loki query path; Loki metric endpoint is standard (`/loki/api/v1/query_range` with metric expressions) |
| Alias implementation adds hidden command-tree complexity | Low -- Cobra supports aliases natively | Use Cobra `Aliases` field for simple alias registration; test both paths |
| `SharedOpts` validation change breaks existing passing commands | Medium -- commands that previously passed with `--from` only now fail | This is the intended fix; add tests for all six time-flag combinations |
| Agent annotations become stale if not updated atomically | Low -- agents generate wrong commands | FR-019 requires annotation updates in the same change as command renames |

## Open Questions

- [RESOLVED]: Should `traces get` also move to `-d` only? -- Yes, it already uses `-d` alongside positional UID. Migration removes the positional UID path; `-d` + single positional `TRACE_ID` is the only form.
- [RESOLVED]: Should `logs query` reject metric LogQL expressions? -- No. `logs query` will continue to accept any LogQL expression; the Loki API handles routing. The fix is that `logs metrics` provides purpose-built formatters, not that `logs query` rejects metric expressions.
- [RESOLVED]: Should `metrics query` split into `metrics query` + `metrics query-range`? -- No. Prometheus instant vs range is reliably deduced from time flag presence, and both response shapes (vector/matrix) use the same structure. A single command is sufficient.
- [DEFERRED]: Should `profiles metrics` support `-o graph` for the `--top` aggregation mode? -- Current `profiles series` already has a graph codec for top mode. Carry forward as-is; enhancements are follow-up work.
