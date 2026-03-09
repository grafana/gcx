# SLO PromQL Patterns

Reference for querying Grafana-generated SLO recording rule metrics.

## Metric Inventory

| Metric | Description | Labels |
|--------|-------------|--------|
| `grafana_slo_sli_window` | SLI over the full objective window (e.g., 28d) | `slo_uuid`, `slo_name` |
| `grafana_slo_sli_1h` | SLI snapshot over the past 1 hour | `slo_uuid`, `slo_name` |
| `grafana_slo_sli_1d` | SLI snapshot over the past 1 day | `slo_uuid`, `slo_name` |
| `grafana_slo_success_rate_5m` | 5-minute success rate (ratio queries only) | `slo_uuid`, `slo_name` |
| `grafana_slo_total_rate_5m` | 5-minute total request rate (ratio queries only) | `slo_uuid`, `slo_name` |
| `grafana_slo_objective` | Configured objective value (0–1) | `slo_uuid`, `slo_name` |

These metrics are written by Grafana recording rules to the destination datasource configured on the SLO. They may not be available on the default Prometheus datasource — always use `.spec.destinationDatasource.uid`.

## Patterns by Use Case

### Current SLI (Full Window)

```promql
# Current SLI value for a specific SLO
grafana_slo_sli_window{slo_uuid="<uuid>"}

# All SLOs — compare SLI to objective
grafana_slo_sli_window / on(slo_uuid) grafana_slo_objective
```

### Short-Window SLI Snapshots

```promql
# 1-hour SLI (fast feedback on recent changes)
grafana_slo_sli_1h{slo_uuid="<uuid>"}

# 1-day SLI (catch gradual degradation)
grafana_slo_sli_1d{slo_uuid="<uuid>"}

# Both together for trend comparison
grafana_slo_sli_1h{slo_uuid="<uuid>"}
grafana_slo_sli_1d{slo_uuid="<uuid>"}
grafana_slo_sli_window{slo_uuid="<uuid>"}
```

### Error Budget

```promql
# Error budget remaining (as fraction, 0–1)
(grafana_slo_sli_window{slo_uuid="<uuid>"} - grafana_slo_objective{slo_uuid="<uuid>"}) /
(1 - grafana_slo_objective{slo_uuid="<uuid>"})

# Error budget consumed (percentage)
(1 - (grafana_slo_sli_window{slo_uuid="<uuid>"} - grafana_slo_objective{slo_uuid="<uuid>"}) /
(1 - grafana_slo_objective{slo_uuid="<uuid>"})) * 100
```

### Burn Rate

Burn rate measures how fast the error budget is being consumed relative to the budget allocation rate.

```promql
# Burn rate = (1 - current_SLI) / (1 - objective)
# A burn rate of 1.0 = consuming budget exactly as fast as it accrues
# A burn rate of 2.0 = consuming budget 2x faster than it accrues
(1 - grafana_slo_sli_1h{slo_uuid="<uuid>"}) /
(1 - grafana_slo_objective{slo_uuid="<uuid>"})

# Short-window burn rate (1h) — used for fast-burn alerting
(1 - grafana_slo_sli_1h{slo_uuid="<uuid>"}) /
(1 - grafana_slo_objective{slo_uuid="<uuid>"})

# Long-window burn rate (24h) — used for slow-burn alerting
(1 - grafana_slo_sli_1d{slo_uuid="<uuid>"}) /
(1 - grafana_slo_objective{slo_uuid="<uuid>"})
```

**Burn rate interpretation:**
- `< 1.0` — budget accruing faster than consumed; on track
- `1.0` — exactly on track to exhaust budget at end of window
- `> 1.0` — breaching; budget will exhaust before window ends
- `> 14.4` — fast-burn threshold for 1h window (burns 30-day budget in 2h)

### Success/Total Rate (Ratio Queries)

```promql
# Real-time success rate (5-minute window)
grafana_slo_success_rate_5m{slo_uuid="<uuid>"}
grafana_slo_total_rate_5m{slo_uuid="<uuid>"}

# Derived error rate (requests/sec failing)
grafana_slo_total_rate_5m{slo_uuid="<uuid>"} - grafana_slo_success_rate_5m{slo_uuid="<uuid>"}

# Success ratio from the 5-minute rates
grafana_slo_success_rate_5m{slo_uuid="<uuid>"} / grafana_slo_total_rate_5m{slo_uuid="<uuid>"}
```

### Objective Value

```promql
# Configured objective for comparison in expressions
grafana_slo_objective{slo_uuid="<uuid>"}

# All SLOs sorted by how far they are from their objective (worst first)
sort_desc(grafana_slo_sli_window - on(slo_uuid) grafana_slo_objective)
```

## Querying with grafanactl

```bash
# Current window SLI
grafanactl query -d <datasource-uid> \
  -e 'grafana_slo_sli_window{slo_uuid="<uuid>"}' \
  --from now-1h --to now --step 1m

# Burn rate over last hour
grafanactl query -d <datasource-uid> \
  -e '(1 - grafana_slo_sli_1h{slo_uuid="<uuid>"}) / (1 - grafana_slo_objective{slo_uuid="<uuid>"})' \
  --from now-1h --to now --step 1m

# Error budget trend (28-day window)
grafanactl query -d <datasource-uid> \
  -e '(grafana_slo_sli_window{slo_uuid="<uuid>"} - grafana_slo_objective{slo_uuid="<uuid>"}) / (1 - grafana_slo_objective{slo_uuid="<uuid>"})' \
  --from now-28d --to now --step 1h
```

## Notes

- `slo_uuid` label matches `.metadata.name` in the SLO definition (UUID format)
- Metrics are only available after recording rules complete their first evaluation (typically 1–2 minutes after SLO creation)
- If metrics return NODATA, verify the destination datasource UID and that Grafana recording rules are active
- For dimensional breakdown during investigation, query the raw success/total metrics directly (not the recording rule aggregates) using selectors from `.spec.query.ratio.successMetric` / `.spec.query.ratio.totalMetric`
