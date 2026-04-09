# Cross-Signal Command Consistency

**Created**: 2026-04-03
**Status**: proposed
**Bead**: none
**Supersedes**: none

## Context

gcx has four signal providers (metrics, logs, profiles, traces) that grew
organically with inconsistent command naming, datasource selection patterns,
and visualization support. PR #341 added Tempo query commands, surfacing these
inconsistencies:

1. **Datasource UID** is a positional `[UID] EXPR` for query commands but a
   `-d` flag for discovery commands. Two patterns to learn, and two adjacent
   bare positional args (`UID EXPR`) are ambiguous to read.

2. **Command naming** diverges across signals: `profiles series` returns time
   series data, `logs series` returns stream metadata (discovery). Same name,
   completely different semantics.

3. **Implicit dispatch** in `metrics query` and `logs query` routes to
   instant/range or log-lines/metric based on flags or expression content.
   This makes `-o graph` unreliable -- the command doesn't know the response
   shape until execution time. In practice, `logs query` with metric LogQL
   produces broken output (numeric values shoehorned into stream format).

4. **Traces** introduced `search`/`metrics` as explicit separate commands
   (matching the `profiles query`/`series` split) but used different naming
   than the rest of the CLI.

## Decision

### 1. `-d/--datasource` everywhere

All commands use `-d/--datasource` flag with config fallback. No positional UIDs.

```bash
gcx traces query '{ status = error }'              # default datasource
gcx traces query -d tempo-uid '{ status = error }'  # explicit
gcx metrics query -d prom-uid 'up'                   # explicit
gcx traces labels -d tempo-uid                       # same pattern
```

The expression (or trace ID) is always the sole positional arg.

**Rejected:** Positional `[UID] EXPR`. Requires context to distinguish two bare
args; creates a split between query commands (positional) and discovery commands
(flag). The `-d` pattern is uniform and self-documenting.

**Rejected:** Unify to positionals everywhere. Discovery commands like `labels`
have ambiguous positional slots when UID is also positional.

**Exception:** `gcx datasources query DATASOURCE_UID EXPR` keeps positional UID.
This is a low-level escape hatch for unsupported datasource types where UID is
always required (no config default). Both args are mandatory, so there is no
optional-positional ambiguity.

### 2. Consistent naming where response shapes and user intent align

The goal is not identical command trees across signals -- each backend has
different capabilities. The goal is that the same verb means the same thing
everywhere it appears. Each signal has a `query` command for its primary
operation, and a `metrics` command where the backend supports a distinct
time-series-over-time query with a different response shape:

```
gcx metrics                       gcx logs                        gcx profiles                    gcx traces
+-- query EXPR      (-d)          +-- query EXPR      (-d)        +-- query EXPR      (-d)        +-- query TRACEQL      (-d)
|                                 +-- metrics EXPR    (-d)        +-- metrics EXPR    (-d)        |   (also: search)
+-- labels          (-d)          +-- labels          (-d)        +-- labels          (-d)        +-- metrics TRACEQL    (-d)
+-- metadata        (-d)          +-- series          (-d)        +-- profile-types   (-d)        +-- labels             (-d)
|                                 |                               |                               |   (also: tags)
'-- adaptive/                     '-- adaptive/                   '-- adaptive (stub)             +-- get TRACE_ID       (-d)
                                                                                                  '-- adaptive/
```

`metrics query` keeps a single command because Prometheus instant vs range is
reliably deduced from `--from`/`--to` presence, and the response shape (vector
vs matrix) is the same structure. The other signals have genuinely different
response shapes between their primary query and time-series query.

**Rejected:** Single `query` command with auto-detect regex for search vs metrics
(original design). Each query type has different response shapes, different
applicable flags, and different `-o graph` visualizations. Explicit commands give
each mode purpose-built formatters (as demonstrated by `slo reports status` vs
`slo reports timeline`).

**Rejected:** `query` + `query-range` naming. Inconsistent across signals --
logs and traces don't naturally map to "range" terminology.

### 3. Rename `profiles series` to `profiles metrics`

The current `profiles series` returns time-series data (Pyroscope SelectSeries
API) -- how a profile metric changes over time. `logs series` returns stream
metadata (Loki series API) -- which label combinations match a selector. Same
command name, completely different semantics.

Renaming `profiles series` to `profiles metrics` resolves the collision and aligns
with the cross-signal convention: `metrics` = time series over time.

### 4. Add `logs metrics` command

New command for metric LogQL queries (`count_over_time`, `rate`, `sum`, etc.).
Currently `logs query` accepts these but produces broken output -- the Grafana
response converter forces numeric time series into the log stream format, losing
labels and rendering raw numbers without context.

`logs metrics` will have proper time-series formatters and `-o graph` support.

### 5. Traces: `labels -l` for tag value discovery, keep `--instant`

Merge `tags` and `tag-values` into a single `labels` command with `-l/--label`
flag, matching the pattern used by metrics, logs, and profiles:

```bash
gcx traces labels                                  # list all tags
gcx traces labels -l service.name                  # values for a tag
gcx traces labels -l service.name --scope span     # scoped values
gcx traces labels -q '{ status = error }'          # filtered tags
```

`labels -l NAME` is the canonical form for fetching values (consistent across all
signals). `tags` is a non-deprecated alias for `labels` (Tempo users naturally
think in "tags"), so `tags -l NAME` also works. `tag-values` is dropped.

**Rejected:** Keeping both `labels -l NAME` and a separate `label-values NAME`
command. Gives users two ways to do the same thing with no clear winner.

Keep the `--instant` flag on `traces metrics`. Instant vs range is inferred
from time flag presence — matching how `metrics query` (Prometheus) works —
and `--instant` lets callers override to instant even when a time range is
provided.

Tempo's instant query computes a single value across a selected time range and
accepts `--since` / `--from` / `--to`. When no time flags are set, both instant
and range queries default to the last hour. The difference from Prometheus is
only in what the API returns: a single data point vs a time series.

**Time flag rules** (consistent with all other signals):

| Flags provided | Query mode |
|----------------|------------|
| (none) | Instant over last hour (inferred) |
| `--instant` | Instant over last hour (explicit) |
| `--since 1h` | Range over last hour |
| `--instant --since 1h` | Instant over last hour |
| `--from X --to Y` | Range |
| `--instant --from X --to Y` | Instant over that range |
| `--step` alone | Error: instant inferred, step not supported with instant |
| `--instant --step ...` | Error: step not supported with instant |
| `--from X` alone | Error: `--to` is required when `--from` is set |
| `--to Y` alone | Error: `--from` is required when `--to` is set |
| `--since` + `--from` | Error: mutually exclusive |

### 6. Aliases and clean breaks

We are pre-GA -- no deprecated aliases, just clean renames where needed.

**Traces aliases (non-deprecated, kept permanently):**

| Alias | Target | Reason |
|-------|--------|--------|
| `search` | `query` | "Search for traces" is natural Tempo UX |
| `tags` | `labels` | Tempo users think in "tags", not "labels" |

**Clean drops (no alias):**

| Removed | Replacement | Reason |
|---------|-------------|--------|
| `traces tag-values TAG` | `traces labels -l TAG` (or `tags -l TAG`) | Consolidated into `labels` with `-l` flag |
| `profiles series` | `profiles metrics` | Resolves naming collision with `logs series` (discovery) |

### Signal-specific flags unchanged

Each signal keeps its domain-specific flags:

- `--profile-type`, `--max-nodes` (profiles)
- `--top`, `--group-by`, `--aggregation` (profiles metrics)
- `--scope`, `-q/--query` (traces labels)
- `--llm` (traces get)
- `--limit` (logs query, traces query -- where the backend supports it)
- `-M/--match` (logs series)
- `-m/--metric` (metrics metadata)
- `-l/--label` (all labels commands)

## Consequences

**Positive:**
- One datasource selection pattern to learn and document
- `metrics` consistently means "time series over time" across logs/profiles/traces
- No more name collision between `logs series` (discovery) and `profiles series` (time series)
- Each command knows its response shape, enabling purpose-built `-o graph` visualizations
- Broken metric LogQL output fixed via dedicated `logs metrics` command
- One canonical form for tag/label value discovery (`labels -l NAME`; `tags -l` accepted as alias for traces)

**Negative:**
- Breaking change: positional `[UID] EXPR` becomes `-d UID EXPR` across all query commands
- Breaking change: `profiles series` renamed to `profiles metrics` (no alias)
- `traces search` renamed to `traces query` (non-breaking: `search` kept as alias)
- Breaking change: `traces tag-values` dropped (`labels -l` or `tags -l` replaces it)
- New `logs metrics` command needs implementation (client, formatters, codecs)

### Migration approach

These are intentionally **hard breaking changes with no deprecation window**.
gcx is pre-GA -- there is no backwards compatibility contract. The old positional
UID form, `profiles series`, and `traces tag-values` will stop working immediately.

No shim, no deprecation warning, no dual-form support. This keeps the codebase
simple and avoids indefinitely maintaining compatibility machinery.

**What needs updating alongside the code changes:**
- Command `Use`, `Long`, and `Example` strings in all signal providers
- CLI reference docs (`docs/reference/cli/`)
- Agent annotations (`AnnotationLLMHint`) that show example invocations
- gcx skills (grafanactl plugin) that generate or suggest commands

**Follow-up work:**
- Implement `-d` migration across all signal providers
- Implement `logs metrics` command with proper time-series formatters
- Rename `profiles series` to `profiles metrics`
- Add `search` and `tags` aliases to traces commands
- Update CLI reference docs, agent hints, and skills
