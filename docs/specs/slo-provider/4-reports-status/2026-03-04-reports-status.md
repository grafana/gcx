# Stage 4: Reports Status

Parent: [SLO Provider Plan](../2026-03-04-slo-provider-plan.md)
Depends on: [Stage 2: Reports CRUD](../2-reports-crud/2026-03-04-reports-crud.md), [Stage 3: SLO Definitions Status](../3-definitions-status/2026-03-04-definitions-status.md)

## Context

Aggregate health per report. Depends on Stage 2 (report client) and Stage 3 (status patterns + chart converters). This is the capstone stage — once complete, the SLO provider is feature-complete.

~200 LOC estimated.

## New Files

### `internal/providers/slo/reports/`

| File | Purpose | LOC |
|------|---------|-----|
| `status.go` | `slo reports status` — combined SLI/budget per report | ~120 |
| `status_test.go` | Report status tests | ~80 |

## Modified Files

- `reports/commands.go` — add `status` subcommand

## Architecture

```
slo reports status
    │
    ├─► Report Plugin API (GET /v1/report)
    │   → report definitions (list of SLO UUIDs per report)
    │
    ├─► SLO Plugin API (GET /v1/slo)  [for objective values]
    │   → SLO definitions for all referenced UUIDs
    │
    └─► Prometheus Query Client
        → grafana_slo_sli_window for all referenced SLO UUIDs
        → compute weighted/combined SLI per report
```

### Combined SLI Computation

The Grafana UI computes combined metrics client-side. We replicate this:

- **Ratio-parseable SLOs**: Weighted average of SLIs based on total event rate (`grafana_slo_total_rate_5m`)
- **Freeform SLOs**: Simple average (no event rate denominator available)
- **Combined error budget**: `(combined_SLI - avg_objective) / (1 - avg_objective)`

Only event-based (ratio) SLOs are supported in reports — see Stage 2 data model for details.

### STATUS Column Logic

Same pattern as Stage 3 definitions status:
- If any referenced SLO has lifecycle status `creating`/`error`: show that status
- If combined SLI >= average objective: `OK`
- If combined SLI < average objective: `BREACHING`

## Table Output

### `slo reports status`

```
NAME                    TIME_SPAN   SLOS   COMBINED_SLI   COMBINED_BUDGET   STATUS
Weekly Platform Report  weekly      3      99.82%         36.0%             OK
Monthly Checkout SLOs   monthly     7      99.71%         -29.0%            BREACHING
```

### `slo reports status -o wide`

```
NAME                    TIME_SPAN   SLOS   COMBINED_SLI   COMBINED_BUDGET   STATUS
Weekly Platform Report  weekly      3      99.82%         36.0%             OK
  payment-api-latency                      99.72%         44.0%             OK
  checkout-avail                           99.85%         -50.0%            BREACHING
  user-auth-errors                         99.42%         42.0%             OK
```

## Verification

```bash
make lint && make tests && make build && bin/grafanactl slo reports status
```

Note: Full verification requires a live Grafana instance with SLOs and reports configured.
