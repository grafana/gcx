---
type: feature-spec
title: "Move query under datasources with per-kind subcommands"
status: done
created: 2026-03-18
---

# Move query under datasources with per-kind subcommands

## Problem Statement

`grafanactl query` is a top-level command that semantically belongs under `grafanactl datasources`. This creates two problems:

1. **Inconsistent CLI hierarchy.** Datasource-specific operations (labels, metadata, targets) already live under `grafanactl datasources {prometheus,loki,pyroscope}`, but querying -- the most common datasource operation -- sits at the root level. Users must memorize that query is an exception to the pattern.

2. **No type safety at the command level.** The current `query` command accepts any datasource UID and auto-detects the type via an API call. This means (a) users get no command-line guidance about which query language or flags apply, (b) typos in datasource UIDs produce confusing "unsupported type" errors instead of early validation, and (c) pyroscope-specific flags (`--profile-type`, `--max-nodes`) are exposed globally even though they only apply to one datasource kind.

The current workaround is the status quo: users run `grafanactl query -d UID -e EXPR` and hope the flags they passed match the datasource type they targeted.

## Scope

### In Scope

- Move `query` from a top-level command to `grafanactl datasources query`
- Create per-kind subcommands: `prometheus`, `loki`, `pyroscope`, `tempo` (stub), and `generic`
- Switch datasource UID and expression from flags (`-d`, `-e`) to positional arguments
- Add `--window` flag as a convenience alternative to `--from`/`--to`
- Add `--limit` flag for Loki queries (and for `generic` when targeting Loki datasources)
- Per-kind datasource type validation (e.g., `datasources query loki` MUST reject non-Loki datasource UIDs)
- Add a new `datasources` section to the context config for per-kind default datasource UIDs
- Shared default datasource resolver function in `internal/config/` used by all consumers (query subcommands, synth provider, existing datasource commands)
- Per-kind default datasource resolution using the new `datasources` config section via the shared resolver
- Scope pyroscope-specific flags (`--profile-type`, `--max-nodes`) to only the `pyroscope` subcommand
- Remove the top-level `query` command registration from root
- Update auto-generated CLI docs (`make docs`)
- `generic` subcommand that auto-detects datasource type (preserving current behavior as an escape hatch)
- `tempo` subcommand that returns a "not implemented" error (future-ready stub)

### Out of Scope

- **Tempo query client implementation.** No `internal/query/tempo/` package exists. The `tempo` subcommand is a stub only.
- **SQL or other new datasource kind subcommands.** Future work.
- **Backward compatibility alias for `grafanactl query`.** This is a breaking change by design; the old command path will stop working.
- **Changes to internal query clients** (`internal/query/{prometheus,loki,pyroscope}/`). The existing client APIs (NewClient, Query, Format) remain unchanged. The Loki client already accepts a `Limit` field in `QueryRequest`; the only change is exposing it as a CLI flag.
- **Behavioral changes to existing `datasources prometheus/loki/pyroscope` subcommands** (labels, metadata, targets). Their behavior stays as-is; only their internal datasource UID resolution is updated to use the shared resolver.
- **Migrating existing `default-{kind}-datasource` config keys.** The old flat keys on the Context struct remain supported as a fallback. The new `datasources` section takes precedence when both are set.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Command placement | `datasources query {kind}` | Consistent with existing `datasources {kind} {operation}` pattern; query is a datasource-scoped operation | Source requirements |
| Positional args for UID and EXPR | `query prometheus UID 'EXPR'` | Reduces flag noise for the most common operation; aligns with source requirements showing positional syntax | Source requirements |
| Breaking change (no alias) | Remove top-level `query` entirely | Clean CLI tree; aliases create maintenance burden and confusion. Users get a clear error pointing them to the new path. | Design judgment |
| `generic` subcommand for unknown types | Preserves auto-detect behavior | Provides escape hatch for community datasources and forward compatibility | Source requirements |
| `tempo` as a stub subcommand | Returns "not implemented" error | Makes the command hierarchy future-ready and discoverable without requiring a backend client | Revision feedback |
| `--window` flag | Convenience for `[now-window, now]` | Common pattern; avoids requiring both `--from` and `--to` for simple lookback queries | Source requirements |
| Datasource type validation for typed subcommands | Fetch datasource metadata and reject type mismatch | Prevents confusing errors from sending PromQL to a Loki datasource | Source requirements |
| New `datasources` config section | Dedicated nested section under context config with per-kind default UIDs | Clean separation from top-level context keys; extensible for future datasource kinds without polluting the context namespace | Revision feedback |
| `--limit` flag for Loki and generic | User-configurable query limit, defaulting to 1000 | The Loki client already supports `Limit` in `QueryRequest`; the hardcoded value should be user-configurable | Revision feedback |
| Shared default datasource resolver | Single function in `internal/config/` used by all consumers | Ensures consistent resolution across query subcommands, synth provider, and datasource commands; avoids duplicating precedence logic | Revision feedback |

## Functional Requirements

- **FR-001**: The CLI MUST register a `query` subcommand under `datasources` with the path `grafanactl datasources query`.

- **FR-002**: `grafanactl datasources query` MUST have five subcommands: `prometheus`, `loki`, `pyroscope`, `tempo`, and `generic`.

- **FR-003**: Each subcommand (`prometheus`, `loki`, `pyroscope`, `generic`) MUST accept two positional arguments: `DATASOURCE_UID` (arg 0) and `EXPR` (arg 1).

- **FR-004**: The `DATASOURCE_UID` argument MUST be optional when the corresponding default datasource is configured via the new `datasources` config section or the legacy flat config key. Resolution MUST use the shared resolver (FR-023).

- **FR-005**: The `generic` subcommand MUST auto-detect datasource type via the Grafana API (preserving the current behavior of the top-level `query` command) when no default is configured.

- **FR-006**: For `prometheus`, `loki`, and `pyroscope` subcommands, the command MUST fetch datasource metadata from the Grafana API and reject the request with a clear error if the datasource type does not match the subcommand name.

- **FR-007**: All subcommands (except `tempo`) MUST support the flags `--from`, `--to`, and `--step` with the same time-parsing behavior as the current `query` command (RFC3339, Unix timestamp, relative `now-Xh` syntax).

- **FR-008**: All subcommands (except `tempo`) MUST support a `--window` flag that sets `--from` to `now-{window}` and `--to` to `now`. The `--window` flag MUST be mutually exclusive with `--from` and `--to`.

- **FR-009**: The `pyroscope` subcommand MUST expose `--profile-type` (required) and `--max-nodes` flags. These flags MUST NOT appear on other subcommands except `generic` (see FR-013).

- **FR-010**: All subcommands (except `tempo`) MUST support the output format flags (`-o table`, `-o json`, `-o yaml`, `-o wide`, `-o graph`) with the same codec behavior as the current `query` command.

- **FR-011**: The top-level `grafanactl query` command MUST be removed from the root command registration.

- **FR-012**: The `generic` subcommand MUST support all datasource types that the current `query` command supports (prometheus, loki, pyroscope), falling back to the type-specific logic based on the detected datasource type.

- **FR-013**: The `generic` subcommand MUST also expose `--profile-type`, `--max-nodes`, and `--limit` flags for cases where the user queries a pyroscope or loki datasource via `generic`.

- **FR-014**: Running `grafanactl datasources query` with no subcommand MUST print help text listing the available subcommands.

- **FR-015**: The `EXPR` positional argument MUST be required for all subcommands (except `tempo`). The command MUST return an error if it is missing.

- **FR-016**: Auto-generated CLI reference docs MUST reflect the new command hierarchy after running `make docs`.

- **FR-017**: The `generic` subcommand MUST require `DATASOURCE_UID` as a positional argument. Default datasource resolution MUST NOT apply to `generic`.

- **FR-018**: The `tempo` subcommand MUST accept no positional arguments and no query-specific flags. It MUST return an error with the message "tempo queries are not yet implemented" (exit code 1). It MUST be visible in `--help` output with a short description indicating it is not yet available.

- **FR-019**: The `loki` subcommand MUST expose a `--limit` flag (integer, default 1000) that controls the maximum number of log lines returned. A value of 0 MUST disable the limit (no cap on returned lines). The value MUST be passed through to the Loki client's `QueryRequest.Limit` field.

- **FR-020**: The context config MUST support a new `datasources` section structured as a map of datasource kind to default UID. The YAML representation MUST be:

```yaml
contexts:
  mycontext:
    datasources:
      prometheus: "<uid>"
      loki: "<uid>"
      pyroscope: "<uid>"
```

- **FR-021**: The `datasources` config section MUST be settable via `grafanactl config set contexts.<name>.datasources.<kind> <uid>` and unsettable via `grafanactl config unset contexts.<name>.datasources.<kind>`.

- **FR-022**: When both the new `datasources.{kind}` key and the legacy `default-{kind}-datasource` key are set for the same context, the `datasources.{kind}` key MUST take precedence. This is enforced by the shared resolver (FR-023).

- **FR-023**: The `internal/config` package MUST export a `DefaultDatasourceUID(ctx Context, kind string) string` function (or equivalent method on `Context`) that resolves the default datasource UID for a given kind. Resolution order: (1) `ctx.Datasources[kind]` from the new config section, (2) legacy flat key (`DefaultPrometheusDatasource`, `DefaultLokiDatasource`, `DefaultPyroscopeDatasource`). The first non-empty value wins. All existing consumers (`cmd/grafanactl/datasources/{prometheus,loki,pyroscope}.go`, `cmd/grafanactl/query/command.go`, `internal/providers/synth/checks/status.go`) MUST be migrated to use this resolver instead of directly accessing the legacy fields.

- **FR-024**: Existing `datasources prometheus`, `datasources loki`, and `datasources pyroscope` subcommands MUST use the shared resolver (FR-023) for default datasource UID resolution, gaining automatic support for the new `datasources` config section without behavioral changes.

## Acceptance Criteria

- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query --help`
  THEN the output lists `prometheus`, `loki`, `pyroscope`, `tempo`, and `generic` as available subcommands

- GIVEN a Grafana context with `datasources.prometheus` configured to a valid UID
  WHEN a user runs `grafanactl datasources query prometheus 'up{job="grafana"}'`
  THEN the command executes an instant query against the configured default Prometheus datasource and prints results in table format

- GIVEN a Grafana context with only the legacy `default-prometheus-datasource` key configured
  WHEN a user runs `grafanactl datasources query prometheus 'up{job="grafana"}'`
  THEN the command executes an instant query against the legacy default Prometheus datasource (fallback behavior)

- GIVEN a Grafana context with both `datasources.prometheus` and `default-prometheus-datasource` set to different UIDs
  WHEN a user runs `grafanactl datasources query prometheus 'up'`
  THEN the command uses the UID from `datasources.prometheus` (new key takes precedence)

- GIVEN a valid Prometheus datasource UID `abc123`
  WHEN a user runs `grafanactl datasources query prometheus abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m`
  THEN the command executes a range query and prints results in table format

- GIVEN a valid Prometheus datasource UID `abc123`
  WHEN a user runs `grafanactl datasources query prometheus abc123 'up' --window 1h`
  THEN the command executes a range query with `from=now-1h` and `to=now`

- GIVEN a user provides both `--window` and `--from`
  WHEN the command is invoked
  THEN the command MUST return an error stating that `--window` is mutually exclusive with `--from` and `--to`

- GIVEN a Loki datasource UID `loki-001`
  WHEN a user runs `grafanactl datasources query loki loki-001 '{job="varlogs"}' --from now-1h --to now`
  THEN the command executes a Loki log query with the default limit of 1000 and prints results in table format

- GIVEN a Loki datasource UID `loki-001`
  WHEN a user runs `grafanactl datasources query loki loki-001 '{job="varlogs"}' --from now-1h --to now --limit 500`
  THEN the command executes a Loki log query with a limit of 500

- GIVEN a Loki datasource UID `loki-001`
  WHEN a user runs `grafanactl datasources query loki loki-001 '{job="varlogs"}' --from now-1h --to now --limit 0`
  THEN the command executes a Loki log query with no limit applied (0 means no limit)

- GIVEN a datasource UID `prom-001` that is type `prometheus`
  WHEN a user runs `grafanactl datasources query loki prom-001 '{job="x"}'`
  THEN the command returns an error message containing "datasource prom-001 is type prometheus, not loki"

- GIVEN a Pyroscope datasource UID `pyro-001`
  WHEN a user runs `grafanactl datasources query pyroscope pyro-001 '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now`
  THEN the command executes a Pyroscope query and prints results in table format

- GIVEN a user runs `grafanactl datasources query prometheus`
  WHEN `--profile-type` is passed as a flag
  THEN the command returns an "unknown flag" error

- GIVEN a user runs `grafanactl datasources query prometheus`
  WHEN `--limit` is passed as a flag
  THEN the command returns an "unknown flag" error

- GIVEN a datasource UID `any-001` of any supported type
  WHEN a user runs `grafanactl datasources query generic any-001 'some_expr' --from now-1h --to now`
  THEN the command auto-detects the datasource type and executes the appropriate query

- GIVEN a Loki datasource UID `loki-001`
  WHEN a user runs `grafanactl datasources query generic loki-001 '{job="x"}' --from now-1h --to now --limit 200`
  THEN the command auto-detects Loki type and executes the query with a limit of 200

- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query tempo`
  THEN the command returns an error with the message "tempo queries are not yet implemented" and exits with code 1

- GIVEN the CLI is built
  WHEN a user runs `grafanactl datasources query tempo --help`
  THEN the output describes the tempo subcommand as not yet available

- GIVEN the CLI is built
  WHEN a user runs `grafanactl query`
  THEN the command returns an "unknown command" error (the top-level query command no longer exists)

- GIVEN all output codecs (table, wide, json, yaml, graph)
  WHEN a user runs `grafanactl datasources query prometheus UID 'up' -o {format}`
  THEN the output matches the format produced by the former `grafanactl query` command for the same query and format

- GIVEN the codebase after this change
  WHEN `make docs` is run with `GRAFANACTL_AGENT_MODE=false`
  THEN the generated CLI reference docs reflect `datasources query {prometheus,loki,pyroscope,tempo,generic}` and do NOT contain a top-level `query` command

- GIVEN a fresh config
  WHEN a user runs `grafanactl config set contexts.myctx.datasources.prometheus prom-uid-123`
  THEN the config file contains a `datasources` section under `myctx` with `prometheus: prom-uid-123`

- GIVEN a config with `datasources.loki` set
  WHEN a user runs `grafanactl config unset contexts.myctx.datasources.loki`
  THEN the `loki` key is removed from the `datasources` section

- GIVEN a Grafana context with `datasources.prometheus` configured
  WHEN the synth provider resolves a default Prometheus datasource UID (e.g., `grafanactl synth checks status`)
  THEN it uses the UID from `datasources.prometheus` via the shared resolver

- GIVEN a Grafana context with `datasources.prometheus` configured
  WHEN a user runs `grafanactl datasources prometheus labels` (existing command)
  THEN the command uses the UID from `datasources.prometheus` via the shared resolver

## Negative Constraints

- The `tempo` subcommand MUST NOT accept positional arguments, query flags, or execute any query. It MUST only return a "not implemented" error.
- The implementation MUST NOT change the public API of any internal query client (`internal/query/{prometheus,loki,pyroscope}/`).
- The implementation MUST NOT alter the observable behavior of existing `datasources prometheus`, `datasources loki`, or `datasources pyroscope` subcommands (labels, metadata, targets) beyond adopting the shared datasource resolver for default UID resolution.
- The implementation MUST NOT introduce a backward-compatibility alias or hidden command for the old `grafanactl query` path.
- The `--profile-type` and `--max-nodes` flags MUST NOT be registered on `prometheus`, `loki`, or `tempo` subcommands. They MUST be registered on `pyroscope` and `generic` only.
- The `--limit` flag MUST NOT be registered on `prometheus`, `pyroscope`, or `tempo` subcommands. It MUST be registered on `loki` and `generic` only.
- The implementation MUST NOT use string formatting for PromQL construction (per project conventions; use `promql-builder` if needed).
- The implementation MUST NOT remove the legacy `default-{kind}-datasource` fields from the `Context` struct. They MUST remain functional as a fallback.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking change removes `grafanactl query` | Users with scripts or muscle memory will get errors | Document in changelog; error message for unknown `query` at root level will guide users to `datasources query` |
| Datasource type validation adds an extra API call | Slight latency increase per query invocation for typed subcommands | The API call is lightweight (single datasource GET); `generic` preserves the current single-call behavior |
| Positional args are harder to discover than flags | New users may not know argument order | Help text and examples MUST clearly show `UID EXPR` order; `--help` on each subcommand MUST document positional args |
| `generic` with pyroscope + loki flags creates flag sprawl | `generic` subcommand has more flags than typed subcommands | Acceptable trade-off for escape-hatch functionality; pyroscope and loki flags are clearly documented as kind-specific in help text |
| Two config paths for default datasources (new section + legacy keys) | Confusion about which key is used | Document precedence clearly: `datasources.{kind}` wins over `default-{kind}-datasource`; log a warning when both are set |
| `tempo` stub may confuse users who expect it to work | Users discover the subcommand but cannot use it | Help text and error message MUST clearly state "not yet implemented" |

## Open Questions

- [RESOLVED] Whether to include a `tempo` subcommand: Yes, as a stub returning "not implemented" error. This makes the hierarchy discoverable and future-ready.
- [RESOLVED] Whether to keep backward compatibility alias: No -- clean break, document in changelog.
- [RESOLVED] Should `generic` require `DATASOURCE_UID` even when a default is configured? Yes -- `generic` MUST always require `DATASOURCE_UID` as a positional argument (no default resolution) to avoid ambiguity when multiple defaults are configured. Typed subcommands allow omitting it per FR-004.
- [RESOLVED] Whether to add `--limit` flag for Loki queries: Yes. The `loki` subcommand and the `generic` subcommand MUST expose a `--limit` flag (integer, default 1000). The Loki client already supports `Limit` in `QueryRequest`; the hardcoded value is now user-configurable. A value of 0 means no limit.
- [RESOLVED] Whether to add a new config section for default datasources: Yes. A `datasources` map (kind -> UID) is added to the context config. It takes precedence over the legacy flat keys.
