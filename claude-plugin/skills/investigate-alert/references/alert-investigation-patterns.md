# Alert Investigation Patterns

Reference for investigating Grafana alerts with gcx. Covers the alert
JSON structure, common investigation query patterns, and graph interpretation.

---

## Alert JSON Structure

`gcx alert rules list -o json` returns an array of alert groups. Each
group contains an array of rules:

```json
[
  {
    "name": "MyAlertGroup",
    "file": "grafana",
    "rules": [
      {
        "state": "firing",
        "name": "HighErrorRate",
        "query": "rate(http_requests_total{status=~\"5..\"}[5m]) / rate(http_requests_total[5m]) > 0.05",
        "duration": 300,
        "labels": {
          "severity": "critical",
          "cluster": "us-east-1"
        },
        "annotations": {
          "summary": "High error rate detected on {{ $labels.job }}",
          "description": "Error rate is {{ $value | humanizePercentage }}",
          "runbook_url": "https://github.com/myorg/runbooks/blob/main/alerts/HighErrorRate.md",
          "dashboard_url": "https://grafana.example.com/d/abc123"
        },
        "alerts": [
          {
            "labels": {
              "alertname": "HighErrorRate",
              "job": "api-server",
              "namespace": "production"
            },
            "annotations": { ... },
            "state": "firing",
            "activeAt": "2024-01-15T10:23:45Z",
            "value": "0.08"
          }
        ],
        "type": "alerting",
        "datasourceUID": "prometheus-uid-abc123"
      }
    ]
  }
]
```

### Key Fields

| Field | Description |
|-------|-------------|
| `state` | `firing`, `pending`, `inactive` |
| `type` | `alerting` (fires alerts) or `recording` (pre-calculates metrics) |
| `query` | The PromQL or LogQL expression that drives the alert |
| `datasourceUID` | UID of the datasource to query for investigation |
| `labels` | Rule-level labels (severity, team, cluster) |
| `annotations.runbook_url` | Link to runbook; fetch with `gh api` for GitHub URLs |
| `annotations.dashboard_url` | Link to related Grafana dashboard |
| `alerts[]` | Currently firing alert instances with their label sets and current values |
| `alerts[].activeAt` | When this instance began firing |
| `alerts[].value` | The numeric value that triggered the alert |

### Extracting the Alert Query

```bash
# Get the query for a specific alert
gcx alert rules list -o json | \
  jq -r '.[] | .rules[] | select(.name == "<AlertName>") | .query'

# Get the datasource UID for a specific alert
gcx alert rules list -o json | \
  jq -r '.[] | .rules[] | select(.name == "<AlertName>") | .datasourceUID'

# Get all currently firing instances with their label sets
gcx alert rules list -o json | \
  jq '.[] | .rules[] | select(.name == "<AlertName>") | .alerts[] | select(.state == "firing")'
```

---

## JSON Response Envelopes

Quick reference for `-o json` output to avoid jq guessing:

| Command | Envelope | jq Access Pattern |
|---------|----------|-------------------|
| `alert rules list` | `[{name, rules: [...]}]` | `.[] \| .rules[]` |
| `datasources list` | `{"datasources": [...]}` | `.datasources[]` |
| `query` (Prometheus) | `{"status", "data": {"resultType", "result": [...]}}` | `.data.result[]` |

---

## Common Investigation Query Patterns

### Latency Alerts

For P99/P95 latency alerts:

```bash
# Current latency percentiles
gcx datasources prometheus query <uid> \
  'histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))' \
  --from now-1h --to now --step 1m -o graph

# Latency by endpoint
gcx datasources prometheus query <uid> \
  'histogram_quantile(0.99, sum by(job, handler) (rate(http_request_duration_seconds_bucket[5m])))' \
  --from now-1h --to now --step 1m -o json
```

### Error Rate Alerts

For alerts on HTTP 5xx or error rates:

```bash
# Overall error rate
gcx datasources prometheus query <uid> \
  'rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m])' \
  --from now-1h --to now --step 1m -o graph

# Error rate by service
gcx datasources prometheus query <uid> \
  'sum by(job) (rate(http_requests_total{status=~"5.."}[5m])) / sum by(job) (rate(http_requests_total[5m]))' \
  --from now-1h --to now --step 1m -o json
```

### Resource Exhaustion Alerts

For CPU, memory, or disk alerts:

```bash
# CPU usage by pod
gcx datasources prometheus query <uid> \
  'sum by(pod) (rate(container_cpu_usage_seconds_total[5m]))' \
  --from now-1h --to now --step 1m -o graph

# Memory usage
gcx datasources prometheus query <uid> \
  'container_memory_working_set_bytes{container!=""}' \
  --from now-30m --to now --step 1m -o json

# Disk free percentage
gcx datasources prometheus query <uid> \
  'node_filesystem_avail_bytes / node_filesystem_size_bytes' \
  --from now-6h --to now --step 5m -o graph
```

### Certificate / TLS Alerts

For cert expiry alerts:

```bash
# Days until certificate expiry
gcx datasources prometheus query <uid> \
  '(certmanager_certificate_expiration_timestamp_seconds - time()) / 86400' \
  --from now-1h --to now --step 10m -o json
```

### Availability / SLO Alerts

For availability or SLO breach alerts:

```bash
# Uptime over last hour
gcx datasources prometheus query <uid> \
  'avg_over_time(up[1h])' \
  --from now-6h --to now --step 5m -o graph

# Current up/down status
gcx datasources prometheus query <uid> \
  'up == 0' \
  --from now-15m --to now --step 1m -o json
```

---

## Loki Log Investigation Patterns

After identifying an issue from metrics, correlate with logs:

```bash
# Find error logs for a service
gcx datasources loki query <loki-uid> '{job="api-server"} |= "error"' \
  --from now-1h --to now -o json

# Find logs around the time the alert started firing (replace timestamp)
gcx datasources loki query <loki-uid> '{namespace="production"} |= "error"' \
  --from 2024-01-15T10:00:00Z --to 2024-01-15T10:30:00Z -o json

# Rate of error log lines (for trend analysis)
gcx datasources loki query <loki-uid> 'rate({job="api-server"} |= "error" [5m])' \
  --from now-2h --to now --step 1m -o graph
```

### Querying at Scale

Loki metric queries (`rate()`, `count_over_time()`, etc.) produce one series per unique label combination. At scale this hits series limits (default 20K). Always aggregate:

```bash
# BAD — one series per pod/namespace/level/... combination
gcx datasources loki query <loki-uid> 'count_over_time({job="app"} [5m])'

# GOOD — aggregate down to what you need
gcx datasources loki query <loki-uid> 'sum(count_over_time({job="app"} [5m]))'
gcx datasources loki query <loki-uid> 'sum by(level) (count_over_time({job="app"} | json [5m]))'
gcx datasources loki query <loki-uid> 'topk(10, sum by(pod) (rate({job="app"} [5m])))'
```

Rule of thumb: if your query uses `rate()`, `count_over_time()`, or `bytes_over_time()`, wrap it with `sum()`, `sum by(label)`, or `topk()`.

### Stream Labels vs Extracted Labels

Loki has two kinds of labels — confusing them causes silent failures:

| | Stream labels | Extracted labels |
|---|---|---|
| Set by | Log ingestion config | Parser stages (`| json`, `| logfmt`) |
| Used in | Stream selector `{job="app"}` | Filter expressions after `|` |
| Indexed | Yes (fast) | No (line-by-line scan) |
| Available | Always | Only after parser stage |

Common mistakes:
- Filtering extracted labels in `{}` — fails silently: `{namespace="prod", pod="app-123"}` won't work if `pod` is extracted, not a stream label
- Using `label_format` to rename extracted fields before they're parsed — add the parser stage first
- Assuming a field visible in Grafana Explore is a stream label — check with `gcx datasources loki labels -d <uid>` (only shows stream labels)

---

## Interpreting Graph Output

`-o graph` renders an ASCII time-series chart in the terminal. Key patterns:

| Visual Pattern | Likely Cause |
|----------------|--------------|
| Sudden vertical spike | Deployment, config change, or external event |
| Gradual rising trend | Resource accumulation (memory leak, disk fill) |
| Flat high value | Persistent overload or misconfiguration |
| Periodic spikes | Cron job, scheduled task, or traffic surge |
| Drop to zero then spike | Process restart or deployment rollout |
| Sawtooth pattern | Crash-loop or auto-scaling oscillation |

Use `-o json` after `-o graph` to extract exact values:
```bash
# Get the peak value during the alert window
gcx datasources prometheus query <uid> '<query>' --from now-2h --to now --step 1m -o json | \
  jq '[.data[].values[] | .value] | max'
```

---

## Runbook Fetching

If the alert annotation contains a GitHub runbook URL, fetch it with:

```bash
gh api /repos/<owner>/<repo>/contents/<path> --jq '.content' | base64 -d
```

For non-GitHub URLs, use `curl`:

```bash
curl -s "<runbook_url>"
```

---

## See Also

- [Grafana Alert Rules documentation](https://grafana.com/docs/grafana/latest/alerting/)
- The `setup-gcx` skill for configuring gcx if not yet set up
