# Stage 3: SLO Definitions Status

Parent: [SLO Provider Plan](../2026-03-04-slo-provider-plan.md)
Depends on: [Stage 1: SLO Definitions CRUD](../1-slo-definitions-crud/2026-03-04-slo-definitions-crud.md)

## Context

Hybrid command combining SLO API + PromQL queries for definition health. Introduces the status command pattern and shared chart converters that Stage 4 will reuse.

~350 LOC estimated.

## New Files

### `internal/providers/slo/definitions/`

| File | Purpose | LOC |
|------|---------|-----|
| `status.go` | `slo definitions status` — table/wide/json/graph output | ~200 |
| `status_test.go` | Status command tests | ~100 |

### `internal/providers/slo/` (shared)

| File | Purpose | LOC |
|------|---------|-----|
| `chart.go` | Graph converters: `FromSLOComplianceSummary`, `FromSLOBurnDown` | ~80 |

## Modified Files

- `definitions/commands.go` — add `status` subcommand

## Recording Rule Metrics

The SLO plugin generates these `grafana_slo_*` metrics per SLO via Prometheus recording rules (evaluated every 60s):

### Always generated (11 metrics)

| Metric | Description | Used In |
|--------|-------------|---------|
| `grafana_slo_info` | Informational with UUID, title, dashboard URL, type labels | Discovery |
| `grafana_slo_objective` | Constant: objective value (e.g., 0.995) with window label | Target comparison |
| `grafana_slo_objective_window_seconds` | Objective window in seconds | Window calculations |
| `grafana_slo_sli_window` | SLI over the full objective window | **Default table SLI** |
| `grafana_slo_sli_3d` | SLI over preceding 3 days | Alerting |
| `grafana_slo_sli_1d` | SLI over preceding 1 day | **Wide table** |
| `grafana_slo_sli_6h` | SLI over preceding 6 hours | Alerting |
| `grafana_slo_sli_2h` | SLI over preceding 2 hours | Alerting |
| `grafana_slo_sli_1h` | SLI over preceding 1 hour | **Wide table** |
| `grafana_slo_sli_30m` | SLI over preceding 30 minutes | Alerting |
| `grafana_slo_sli_5m` | SLI over preceding 5 minutes | Graph range queries |

### Ratio-only metrics (2 additional)

| Metric | Description |
|--------|-------------|
| `grafana_slo_success_rate_5m` | SLI numerator (success rate) over 5-min window |
| `grafana_slo_total_rate_5m` | SLI denominator (total rate) over 5-min window |

### Standard labels on all metrics

`grafana_slo_uuid`, `grafana_slo_name`, `grafana_slo_service`, `grafana_slo_team`, `grafana_slo_window`, `grafana_slo_type`, `__grafana_origin`

## Architecture

```
slo definitions status [uuid]
    │
    ├─► SLO Plugin API (GET /v1/slo)
    │   → definitions, objectives, destinationDatasource.uid
    │
    └─► Prometheus Query Client (existing internal/query/prometheus/)
        → grafana_slo_sli_window{grafana_slo_uuid=~"..."}
        → grafana_slo_objective{grafana_slo_uuid=~"..."}
        → (wide) grafana_slo_sli_1h, grafana_slo_sli_1d
```

### Step-by-step Flow

1. Fetch SLO list (or single SLO) from plugin API → definitions, objectives, `destinationDatasource.uid`, `parsesAsRatio`
2. Group SLOs by destination datasource UID (SLOs may target different datasources)
3. For each unique datasource, batch PromQL instant query: `grafana_slo_sli_window{grafana_slo_uuid=~"uuid1|uuid2|uuid3"}`
4. Execute via existing `internal/query/prometheus/Client.Query` using datasource proxy API
5. Compute error budget client-side: `(SLI - objective) / (1 - objective)`
6. Merge API data + metric data, render as table/JSON/graph

### PromQL Query Patterns

**Default table — SLI over objective window (simplified):**
```promql
grafana_slo_sli_window{grafana_slo_uuid=~"uuid1|uuid2|uuid3"}
```

**Error budget remaining (ratio-parseable SLOs, for graph):**
```promql
(
  clamp_max(sum(sum_over_time((grafana_slo_success_rate_5m{grafana_slo_uuid="<UUID>"}[<WINDOW>:5m]))
  / sum(sum_over_time((grafana_slo_total_rate_5m{grafana_slo_uuid="<UUID>"}[<WINDOW>:5m]))), 1)
  - on() grafana_slo_objective{grafana_slo_uuid="<UUID>"}
)
/ on() (1 - grafana_slo_objective{grafana_slo_uuid="<UUID>"})
```

**Error budget burn rate (1h window, for wide table):**
```promql
(
  1 - clamp_max((sum(avg_over_time(grafana_slo_success_rate_5m{grafana_slo_uuid="<UUID>"}[1h]))
  / sum(avg_over_time(grafana_slo_total_rate_5m{grafana_slo_uuid="<UUID>"}[1h]))), 1)
)
/ on() (1 - grafana_slo_objective{grafana_slo_uuid="<UUID>"})
> -Inf
```

**Freeform SLO SLI (when `parsesAsRatio=false`):**
```promql
clamp_max(avg(avg_over_time((grafana_slo_sli_5m{grafana_slo_uuid="<UUID>"}[<WINDOW>:5m])), 1)
```

### STATUS Column Logic

- If `readOnly.status.type` is `creating`/`updating`/`deleting`/`error`: show **lifecycle status** (metric data unavailable)
- If SLI >= objective: `OK`
- If SLI < objective (error budget exhausted): `BREACHING`
- If metric query returns no data: `NODATA`

## Graph Package Enhancements

Optional additions to `internal/graph/`:

- `Thresholds` field on `ChartOptions` for objective reference lines (~30 LOC)
- Y-axis percentage formatter via `YFormatter` field + `WithYLabelFormatter()` (~5 LOC)

### Graph Converter Functions (in `chart.go`)

| Function | Chart Type | Graph Changes |
|----------|-----------|---------------|
| `FromSLOComplianceSummary` | Bar chart (instant — single point per SLO) | None |
| `FromSLOBurnDown` | Line chart (range — budget over time) | None |
| `FromSLOBurnRates` | Bar chart (instant — burn rate per SLO) | None |
| `FromSLOSLITrend` | Line chart (range — SLI over time) | None |

All converters produce `*graph.ChartData` — `RenderChart` auto-dispatches bar vs line based on `IsInstantQuery()`.

## Table Output

### `slo definitions status`

```
NAME                    UUID          OBJECTIVE   WINDOW   SLI       BUDGET    STATUS
payment-api-latency     abc123def     99.50%      28d      99.72%    44.0%     OK
checkout-availability   xyz789ghi     99.90%      28d      99.85%    -50.0%    BREACHING
new-feature-slo         pqr012stu     99.50%      28d      --        --        Creating
```

### `slo definitions status -o wide`

```
NAME                  UUID        OBJECTIVE  WINDOW  SLI      BUDGET   BURN_RATE  SLI_1H   SLI_1D   STATUS
payment-api-latency   abc123def   99.50%     28d     99.72%   44.0%    0.8x       99.91%   99.80%   OK
checkout-availability xyz789ghi   99.90%     28d     99.85%   -50.0%   2.3x       99.70%   99.82%   BREACHING
```

## Verification

```bash
make lint && make tests && make build && bin/gcx slo definitions status
```

Note: Full verification requires a live Grafana instance with SLOs configured.
