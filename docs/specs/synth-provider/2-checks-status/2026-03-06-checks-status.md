# Stage 2: Checks Status + Timeline

Parent: [Synthetic Monitoring Provider Plan](../2026-03-06-synth-provider-plan.md)

> **Status**: DEFERRED — requires Stage 1 completion.

## Context

SM probes publish `probe_success` and `probe_duration_seconds` metrics to the
tenant's Prometheus remote write endpoint. This stage adds two commands that
query these metrics via the Grafana Prometheus datasource proxy.

~400 LOC estimated.

## Commands

### `gcx synth checks status [id]`

Shows current pass/fail status per check (instant query over last 5 minutes).

```
ID      JOB                         TARGET                         TYPE   SUCCESS  PROBES_UP  PROBES_TOTAL
6247    mimir-dev-10 GET root        https://prometheus-dev-10...   http   100.0%   2/2        Oregon, Spain
6248    atlantis /healthz            https://atlantis-webhooks...   http   95.0%    1/2        Oregon
6249    k6 browser smoke             https://app.example.com        browser  --     0/1        Paris (offline)
```

Columns:
- `SUCCESS`: `avg(probe_success{job="<job>", instance="<target>"}[5m])` × 100
- `PROBES_UP` / `PROBES_TOTAL`: count probes reporting vs assigned

### `gcx synth checks timeline <id> [--window 6h]`

Shows pass/fail over time for a single check as a terminal line chart.

```
gcx synth checks timeline 6247 --window 6h
```

Output: time-series line chart (reusing `internal/graph` package):
- X axis: time
- Y axis: probe_success (0.0–1.0)
- One line per probe

```
probe_success for "mimir-dev-10 GET root" (6h window)
1.0 ┤
    │▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓  Oregon
0.5 ┤
    │▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓  Spain
0.0 └─────────────────────────────────────────────────
    18:00                 21:00                 00:00
```

## Prometheus Queries

SM metrics use these labels:
- `job` — matches check's `job` field
- `instance` — matches check's `target` field
- `probe` — probe name (e.g. "Oregon", "Spain")

```promql
# Status: instant success rate per check
avg by (job, instance) (avg_over_time(probe_success{job="<job>", instance="<target>"}[5m]))

# Timeline: per-probe success over time window
probe_success{job="<job>", instance="<target>"}
```

## Prometheus Datasource Access

Same pattern as SLO status commands:
- Use gcx context's Grafana server + token
- Query via Grafana's Prometheus datasource proxy API
- Datasource UID from config (or auto-detected if only one Prometheus datasource exists)

## New Files

| File | Purpose | LOC |
|------|---------|-----|
| `checks/status.go` | status + timeline commands, Prometheus query logic | ~250 |
| `checks/status_test.go` | tests with mock Prometheus | ~150 |

## Dependencies on Stage 1

- `checks/client.go` — needs List() to resolve IDs to job+target for queries
- `probes/client.go` — needs List() to show probe names alongside results
- `provider.go` / `configLoader` — needs gcx Grafana context for Prometheus access

## Verification

```bash
make lint && make tests && make build
```

Live smoke tests (load credentials from `.env`):
```bash
source .env

# Status — all checks
bin/gcx synth checks status
bin/gcx synth checks status -o json

# Status — single check (pick an ID from `synth checks list`)
FIRST_ID=$(bin/gcx synth checks list -o json | jq -r '.[0].metadata.name')
bin/gcx synth checks status "$FIRST_ID"

# Timeline — single check, default window
bin/gcx synth checks timeline "$FIRST_ID"

# Timeline — custom window
bin/gcx synth checks timeline "$FIRST_ID" --window 1h
bin/gcx synth checks timeline "$FIRST_ID" --window 24h
```
