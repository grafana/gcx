---
name: debug-with-grafana
description: Structured diagnostic workflow for debugging application issues using Grafana observability data. Use when the user reports errors, latency spikes, service degradation, HTTP 500s, or wants to investigate why a service is behaving unexpectedly. Triggers for: "my API is returning 500 errors", "latency is spiking", "service seems down", "help me debug using Grafana", "investigate why requests are failing", "something is wrong with my service".
---

# Debug with Grafana

A structured 7-step diagnostic workflow for debugging application issues using
Prometheus metrics, Loki logs, and Grafana resources. Follow steps in order —
each step informs the next.

## Prerequisites

gcx must be installed and configured with a valid context before running
any commands. If not configured, use the `setup-gcx` skill first:

```bash
# Verify configuration
gcx config view

# Switch context if needed
gcx config use-context <context-name>
```

## Diagnostic Workflow

### Step 1: Discover Datasources

List all available datasources to identify Prometheus and Loki UIDs. All
subsequent query commands require a datasource UID via `-d <uid>`.

```bash
# List all datasources
gcx datasources list -o json

# Filter by type for scripting
gcx datasources list -t prometheus -o json
gcx datasources list -t loki -o json

# Capture UIDs for use in subsequent steps
PROM_UID=$(gcx datasources list -t prometheus -o json | jq -r '.datasources[0].uid')
LOKI_UID=$(gcx datasources list -t loki -o json | jq -r '.datasources[0].uid')
```

**Expected output shape:**
```json
{
  "datasources": [
    {"uid": "<uid>", "name": "<display-name>", "type": "prometheus", ...},
    {"uid": "<uid>", "name": "<display-name>", "type": "loki", ...}
  ]
}
```

If no datasources appear, confirm the context is pointing at the correct
Grafana instance. See `references/error-recovery.md` for auth and
datasource-not-found recovery patterns.

### Step 2: Confirm Data Availability

Before querying specific metrics, confirm the target service is instrumented
and data is flowing. This avoids wasting time on empty results.

```bash
# Check that the target service is being scraped
gcx metrics targets -d <prom-uid> -o json

# Verify the relevant job label exists
gcx metrics labels -d <prom-uid> -l job -o json

# For Loki: confirm log streams exist for the service
gcx logs labels -d <loki-uid> -l job -o json
gcx logs series -d <loki-uid> -M '{job="<service-name>"}' -o json

# Spot-check: confirm uptime metrics are present for the service
gcx metrics query <prom-uid> 'up{job="<service-name>"}' -o json
```

**Expected output shape:**
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"__name__": "up", "job": "<service-name>", "instance": "<host:port>"}, "value": [<timestamp>, "<0-or-1>"]}
    ]
  }
}
```

A `value` of `"0"` means the service is down or not being scraped. Empty
`result` array means the metric is absent — see Failure Mode 3 in
`references/error-recovery.md`.

### Step 3: Query Error Rates

Query the HTTP 5xx error rate over the relevant time window to establish
whether an error spike exists and when it began.

```bash
# HTTP 5xx error rate (range query for trend)
gcx metrics query <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o json

# Visualize the trend
gcx metrics query <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o graph

# Error ratio (errors / total)
gcx metrics query <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m]) / rate(http_requests_total{job="<service-name>"}[5m])' \
  --from now-1h --to now --step 1m -o json

# Break down by status code to identify 500 vs 503 vs 504
gcx metrics query <prom-uid> \
  'sum by(status) (rate(http_requests_total{job="<service-name>"}[5m]))' \
  --from now-1h --to now --step 1m -o json
```

**Expected output shape (matrix for range queries):**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "<service-name>", "status": "<code>"},
        "values": [[<timestamp>, "<rate>"], ...]
      }
    ]
  }
}
```

Note the timestamp where the rate increases — this is the incident start time.
Use this window in subsequent steps.

### Step 4: Query Latency

Query request latency to determine whether the service is slow (latency issue)
or failing fast (error issue). High latency often precedes error spikes.

```bash
# P50/P95/P99 latency from histogram
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="<service-name>"}[5m]))' \
  --from now-1h --to now --step 1m -o json

# Visualize P95 latency trend
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="<service-name>"}[5m]))' \
  --from now-1h --to now --step 1m -o graph

# Average latency as a simpler signal if histograms are unavailable
gcx metrics query <prom-uid> \
  'rate(http_request_duration_seconds_sum{job="<service-name>"}[5m]) / rate(http_request_duration_seconds_count{job="<service-name>"}[5m])' \
  --from now-1h --to now --step 1m -o json

# Latency by endpoint (if label available)
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, sum by(le, handler) (rate(http_request_duration_seconds_bucket{job="<service-name>"}[5m])))' \
  --from now-1h --to now --step 1m -o json
```

**Expected output shape:**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "<service-name>"},
        "values": [[<timestamp>, "<seconds>"], ...]
      }
    ]
  }
}
```

Compare the latency onset time with the error onset time from Step 3. If
latency rose before errors, a dependency or resource constraint is likely.

### Step 5: Correlate Logs

Query Loki for error logs in the time window identified in Steps 3 and 4.
Logs provide the specific error messages, stack traces, and context that
metrics cannot.

```bash
# Error logs for the service in the incident window
gcx logs query <loki-uid> \
  '{job="<service-name>"} |= "error"' \
  --from now-1h --to now -o json

# JSON-parsed logs with level filter (if structured logging)
gcx logs query <loki-uid> \
  '{job="<service-name>"} | json | level="error"' \
  --from now-1h --to now -o json

# Error rate from logs (count over time)
gcx logs query <loki-uid> \
  'count_over_time({job="<service-name>"} |= "error" [5m])' \
  --from now-1h --to now --step 1m -o json

# Grep for specific error patterns
gcx logs query <loki-uid> \
  '{job="<service-name>"} |~ "timeout|connection refused|OOM|panic"' \
  --from now-1h --to now -o json
```

**Expected output shape (streams):**
```json
{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [
      {
        "stream": {"job": "<service-name>", "level": "<level>"},
        "values": [["<ns-timestamp>", "<log-line>"], ...]
      }
    ]
  }
}
```

Look for:
- Repeated error messages pointing to a specific code path or dependency
- Timestamps of first error matching the metric spike time from Step 3
- Stack traces or panic messages that identify the root cause
- Upstream service names in error messages (database, external APIs)

### Step 6: Check Related Dashboards and Resources

Check whether relevant dashboards exist that give broader context, and inspect
related Grafana resources that may explain the issue (e.g., alert rules that
are firing).

```bash
# List all alert rules to find any firing for this service
gcx alert rules list -o json | jq '.[] | .rules[]? | select(.labels.job == "<service-name>")'

# Pull dashboards locally to inspect their panel queries
gcx resources pull dashboards -o json

# List available resources to find service-specific dashboards
gcx resources get dashboards -o json | jq '.items[] | select(.metadata.name | test("<service-name>"; "i"))'

# If a relevant dashboard UID is known, get it directly
gcx resources get dashboards/<dashboard-uid> -o json
```

#### Capture a visual snapshot of a relevant dashboard

If a relevant dashboard UID is known, capture a PNG snapshot to visually
inspect panel layout and current state. This is especially useful when
diagnosing layout regressions, missing data, or anomalous panel values.

```bash
# First, discover which template variables the dashboard uses so you can
# pin them to the values relevant to the incident being debugged
gcx resources get dashboards/<dashboard-uid> -ojson | \
  jq '.spec.templating.list[] | {name, type, current: .current.value}'

# Capture a full dashboard snapshot with variables matching the incident context
# (requires grafana-image-renderer plugin on the Grafana instance)
gcx dashboards snapshot <dashboard-uid> --output-dir ./debug-snapshots \
  --var cluster=<cluster> --var job=<service-name> --since 1h

# Capture the incident time window explicitly
gcx dashboards snapshot <dashboard-uid> --from now-1h --to now \
  --var cluster=<cluster> --var job=<service-name> --output-dir ./debug-snapshots

# Capture a specific panel (find panel IDs: .spec.panels[].id in the dashboard JSON)
gcx dashboards snapshot <dashboard-uid> --panel <panel-id> \
  --output-dir ./debug-snapshots

# If stuck with flags: gcx dashboards snapshot --help
```

Cross-reference with metrics and logs:
- Are there alert rules in a firing or pending state for this service?
- Do existing dashboards show additional signals (queue depth, DB connections,
  memory pressure)?
- Do dashboard panel queries reveal which metrics are being monitored?
- Does the dashboard snapshot show unexpected panel states or missing data?

### Step 7: Summarize Findings

After completing Steps 1-6, synthesize the findings into a clear diagnostic
summary for the user.

Structure the summary as:

```
Service: <service-name>
Time window: <from> to <to>
Incident start: <timestamp from error rate onset>

Error signal:
  - Error rate: <trend description, not fabricated value>
  - Status codes: <which codes are elevated>

Latency signal:
  - P95 latency: <trend description>
  - Latency onset: <before/after/same time as errors>

Log evidence:
  - Error pattern: <recurring message or exception>
  - First occurrence: <timestamp>
  - Frequency: <how often in the window>

Related resources:
  - Firing alerts: <names or "none found">
  - Relevant dashboards: <names or UIDs>

Likely root cause:
  - <Primary hypothesis based on all signals>

Recommended next actions:
  1. <Specific action — check dependency, review deploy, inspect resource usage>
  2. <Additional action>
```

Use `-o graph` for any visualizations shared with the user. Use `-o json` for
data retrieved for your own analysis.

---

## Example Scenarios

### Scenario 1: HTTP 500 Error Spike

**Trigger**: User reports "my API started returning 500 errors 30 minutes ago".

**Command sequence:**

```bash
# Step 1: Find datasource UIDs
gcx datasources list -t prometheus -o json
gcx datasources list -t loki -o json

# Step 2: Confirm service is being scraped
gcx metrics query <prom-uid> 'up{job="api"}' -o json

# Step 3: Observe error rate over last 2 hours (wider window to see the spike start)
gcx metrics query <prom-uid> \
  'rate(http_requests_total{job="api",status=~"5.."}[5m])' \
  --from now-2h --to now --step 1m -o graph

# Identify which status codes are elevated
gcx metrics query <prom-uid> \
  'sum by(status) (rate(http_requests_total{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o json

# Step 4: Check if latency rose at the same time
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o graph

# Step 5: Get error logs in the spike window
gcx logs query <loki-uid> \
  '{job="api"} |= "error"' \
  --from now-2h --to now -o json

# Step 6: Check alert rules
gcx alert rules list -o json | jq '.[] | .rules[]? | select(.state == "firing")'
```

**Expected output shape at Step 3 (matrix):**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "api", "status": "500"},
        "values": [[<timestamp>, "<rate>"], ...]
      }
    ]
  }
}
```

**Interpretation**: Look for the timestamp where `values` shows the rate
increasing from baseline. Match this to log timestamps in Step 5.

---

### Scenario 2: Latency Degradation

**Trigger**: User reports "requests are taking much longer than usual, no errors yet".

**Command sequence:**

```bash
# Step 1: Find datasource UIDs
gcx datasources list -t prometheus -o json

# Step 2: Confirm service health (latency without errors suggests slow dependency)
gcx metrics query <prom-uid> 'up{job="api"}' -o json

# Step 3: Error rate (confirm it's not elevated yet)
gcx metrics query <prom-uid> \
  'rate(http_requests_total{job="api",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o json

# Step 4: P95 latency is the primary signal — visualize trend
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o graph

# Break down by endpoint to isolate which routes are slow
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, sum by(le, handler) (rate(http_request_duration_seconds_bucket{job="api"}[5m])))' \
  --from now-1h --to now --step 1m -o json

# Step 5: Check for timeout log patterns suggesting upstream dependency issue
gcx logs query <loki-uid> \
  '{job="api"} |~ "timeout|slow|waiting"' \
  --from now-2h --to now -o json

# Check database or downstream service latency if metrics available
gcx metrics query <prom-uid> \
  'rate(db_query_duration_seconds_sum{job="api"}[5m]) / rate(db_query_duration_seconds_count{job="api"}[5m])' \
  --from now-2h --to now --step 1m -o json
```

**Expected output shape at Step 4 (histogram):**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "api"},
        "values": [[<timestamp>, "<seconds>"], ...]
      }
    ]
  }
}
```

**Interpretation**: Rising `values` across all endpoints suggests a shared
resource or dependency. Rising values for one endpoint only suggests a
handler-specific issue. Compare latency onset time with log timestamps.

---

### Scenario 3: Service Down / No Data

**Trigger**: User reports "service seems completely down" or dashboard shows no data.

**Command sequence:**

```bash
# Step 1: Verify datasource connectivity first (simplest possible query)
gcx datasources list -o json

# Step 2: Check whether the service is being scraped at all
gcx metrics targets -d <prom-uid> -o json | \
  jq '.[] | select(.labels.job == "api")'

# Confirm up metric — value "0" means scrape failure, absent means not scraped
gcx metrics query <prom-uid> 'up{job="api"}' -o json

# Check if the job label exists at all (absence = service was never registered)
gcx metrics labels -d <prom-uid> -l job -o json

# Step 3: Without error rate data, check for recent data gaps
gcx metrics query <prom-uid> \
  'absent(up{job="api"})' \
  --from now-1h --to now --step 1m -o json

# Step 4: Query latency from any recent data before the outage
gcx metrics query <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-3h --to now --step 5m -o graph

# Step 5: Check Loki for last known logs before data disappeared
gcx logs query <loki-uid> \
  '{job="api"}' \
  --from now-3h --to now -o json

# Crash or OOM signals in logs
gcx logs query <loki-uid> \
  '{job="api"} |~ "panic|OOM|killed|crashed|SIGTERM"' \
  --from now-3h --to now -o json

# Step 6: Check alert rules for any firing service-down alerts
gcx alert rules list -o json | jq '.[] | .rules[]? | select(.state == "firing")'
```

**Expected output shape when service is down (up=0):**
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {"__name__": "up", "job": "api", "instance": "<host:port>"},
        "value": [<timestamp>, "0"]
      }
    ]
  }
}
```

**Expected output shape when service was never scraped (absent):**
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": []
  }
}
```

**Interpretation**:
- `up=0`: Service is registered but failing health checks — check pod/process status
- Empty result for `up{job="api"}`: Job never existed or was removed from scrape config
- Data present up to a specific timestamp then absent: Service crashed at that time — correlate with crash logs

---

## References

- [`references/error-recovery.md`](references/error-recovery.md) — Recovery
  patterns for auth errors (401/403), datasource not found, empty results,
  query timeouts, and malformed PromQL/LogQL syntax.

- [`references/query-patterns.md`](references/query-patterns.md) — Advanced
  query patterns for Prometheus and Loki datasources, including time range
  formats, aggregation patterns, Loki stream operators, and output format
  reference.
