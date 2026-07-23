---
name: debug-with-grafana
description: >
  Structured workflow for investigating application problems with Grafana
  observability data (metrics, logs, traces) via gcx. Covers live
  firefighting AND retrospective incident analysis: incident triage,
  root-cause analysis, blast-radius checks (did an incident spill into
  other services), verifying whether a deployment or rollout triggered an
  incident, finding which service, endpoint, or path owns the most errors
  or slow requests, checking whether retries or queue backlogs piled up,
  and quantifying error or latency shares over a time window. Trigger on:
  "my API is returning 500 errors", "latency is spiking", "investigate why
  requests are failing", "triage the incident", "blast radius", "root
  cause", "did the rollout cause it", "which endpoint owns the most 5xx",
  "did retries pile up", or any request to analyse an earlier incident
  window using telemetry. For authoring dashboards use create-dashboard;
  for dashboard inventory use manage-dashboards.
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
PROM_UID=$(gcx datasources list -t prometheus -o json 2>/dev/null | \
  python3 -c "import json,sys; print(json.load(sys.stdin)['datasources'][0]['uid'])")
LOKI_UID=$(gcx datasources list -t loki -o json 2>/dev/null | \
  python3 -c "import json,sys; print(json.load(sys.stdin)['datasources'][0]['uid'])")
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

> **JSON output piping**: When piping gcx output through external tools, never
> use `2>&1` — gcx writes hints to stderr that break JSON parsers. Use
> `2>/dev/null` to suppress stderr, or use `--json field1,field2` to select
> fields directly without piping:
> ```bash
> gcx datasources list -t prometheus --json uid
> gcx metrics query -d <prom-uid> 'up' --json metric,value
> ```
> Use `--json list` to discover available fields for any command.

### Step 2: Confirm Data Availability

Before querying specific metrics, confirm the target service is instrumented
and data is flowing. This avoids wasting time on empty results.

```bash
# Check that the target service is being scraped
gcx metrics query -d <prom-uid> 'up' -o json

# Verify the relevant job label exists
gcx metrics labels -d <prom-uid> -l job -o json

# For Loki: confirm log streams exist for the service
gcx logs labels -d <loki-uid> -l job -o json
gcx logs series -d <loki-uid> -M '{job="<service-name>"}' -o json

# Spot-check: confirm uptime metrics are present for the service
gcx metrics query -d <prom-uid> 'up{job="<service-name>"}' -o json
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
gcx metrics query -d <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o json

# Visualize the trend
gcx metrics query -d <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o graph

# Error ratio (errors / total)
gcx metrics query -d <prom-uid> \
  'rate(http_requests_total{job="<service-name>",status=~"5.."}[5m]) / rate(http_requests_total{job="<service-name>"}[5m])' \
  --from now-1h --to now --step 1m -o json

# Break down by status code to identify 500 vs 503 vs 504
gcx metrics query -d <prom-uid> \
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
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="<service-name>"}[5m]))' \
  --from now-1h --to now --step 1m -o json

# Visualize P95 latency trend
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="<service-name>"}[5m]))' \
  --from now-1h --to now --step 1m -o graph

# Average latency as a simpler signal if histograms are unavailable
gcx metrics query -d <prom-uid> \
  'rate(http_request_duration_seconds_sum{job="<service-name>"}[5m]) / rate(http_request_duration_seconds_count{job="<service-name>"}[5m])' \
  --from now-1h --to now --step 1m -o json

# Latency by endpoint (if label available)
gcx metrics query -d <prom-uid> \
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
gcx logs query -d <loki-uid> \
  '{job="<service-name>"} |= "error"' \
  --from now-1h --to now -o json

# JSON-parsed logs with level filter (if structured logging)
gcx logs query -d <loki-uid> \
  '{job="<service-name>"} | json | level="error"' \
  --from now-1h --to now -o json

# Error rate from logs (count over time)
gcx logs query -d <loki-uid> \
  'count_over_time({job="<service-name>"} |= "error" [5m])' \
  --from now-1h --to now --step 1m -o json

# Grep for specific error patterns
gcx logs query -d <loki-uid> \
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
        "stream": {"job": "<service-name>"},
        "values": [
          {
            "timestamp": "<ns-timestamp>",
            "line": "<log-line>",
            "structuredMetadata": {"detected_level": "error"},
            "parsed": {"status": "500"}
          }
        ]
      }
    ]
  }
}
```
`stream` holds indexed labels only (valid inside `{...}`). `structuredMetadata`
(per-line, e.g. `detected_level`) and `parsed` (from `| json` / `| logfmt`) are
present only when the line has them, and are usable only after a pipe — not in a
`{}` selector.

> **LogQL pitfall**: Loki requires at least one non-empty label matcher in the
> stream selector. `{}` and `{} |~ "pattern"` will be rejected. Always include
> at least one label, e.g., `{job=~".+"}` as a catch-all.
>
> **Label-kind pitfall**: only *indexed* labels are valid inside `{...}`. Loki's
> structured metadata (e.g. its auto-added `detected_level`) and parsed fields
> are NOT — `{detected_level="error"}` returns zero streams silently. Filter
> them after a pipe: `{job="app"} | detected_level="error"`. In `gcx logs query`
> output, indexed labels are in the `stream` map / `STREAM` column; structured
> metadata and parsed labels are in per-entry `structuredMetadata` / `parsed`
> (`-o json`) or `DETAILS` (`-o table`).

Look for:
- Repeated error messages pointing to a specific code path or dependency
- Timestamps of first error matching the metric spike time from Step 3
- Stack traces or panic messages that identify the root cause
- Upstream service names in error messages (database, external APIs)

### Step 5b: Correlate Traces (if Tempo is available)

If a Tempo datasource exists, search for traces matching the incident window.
Traces show individual request paths and identify slow or failing spans.

```bash
# Check for Tempo datasources
gcx datasources list -t tempo -o json

# Search for error traces in the incident window
gcx traces query -d <tempo-uid> '{ status = error }' --from now-1h --to now

# Search by service name
gcx traces query -d <tempo-uid> '{ resource.service.name = "<service-name>" }' --from now-1h --to now

# Search for slow traces (duration > 1s)
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service-name>" && duration > 1s }' \
  --from now-1h --to now

# Fetch a specific trace by ID for analysis (from search results or log trace IDs)
# Always use --llm so Tempo returns its token-efficient LLM trace encoding.
gcx traces get -d <tempo-uid> <trace-id> --llm -o json
```

**TraceQL attribute scoping**: Tempo requires scoped attribute names. Use
`resource.` for resource-level attributes and `span.` for span-level:
- `resource.service.name` (not `service.name`)
- `span.http.status_code` (not `http.status_code`)

Use `name` (unscoped) for the span name, `duration` for span duration,
and `status` for span status. Use `trace:rootService` and `trace:rootName`
for root span attributes (not `rootServiceName` or `rootTraceName`).

When inspecting trace bodies, use `gcx traces get <trace-id> --llm -o json`. Do not fetch the
OTLP-shaped default trace and manually compact it unless the user explicitly
needs raw trace JSON for schema/debugging work.

Discover available labels and values:
```bash
gcx traces labels -d <tempo-uid>
# For agent workflows, request Tempo's compact LLM label-value encoding.
gcx traces tags -d <tempo-uid> -l resource.service.name --llm -o json
```

> **Common mistake**: `gcx traces labels -l service.name` will fail — Tempo
> parses the dot as an identifier boundary. Always fully qualify:
> `-l resource.service.name`, not `-l service.name`.

See [`references/traceql-patterns.md`](references/traceql-patterns.md) for full
TraceQL syntax reference.

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

For complete end-to-end command sequences mapped to the steps above, see
[`references/example-scenarios.md`](references/example-scenarios.md):

- **Scenario 1: HTTP 500 error spike** - error rate trend, status-code
  breakdown, log correlation
- **Scenario 2: Latency degradation** - P95 trend, per-endpoint breakdown,
  dependency latency
- **Scenario 3: Service down / no data** - `up` checks, `absent()`, crash
  signals in logs

Read the matching scenario when starting an investigation of that shape;
otherwise follow the numbered workflow directly.

---

## References

- [`references/example-scenarios.md`](references/example-scenarios.md) - Full
  command sequences for the three common scenarios (error spike, latency
  degradation, service down).

- [`references/error-recovery.md`](references/error-recovery.md) — Recovery
  patterns for auth errors (401/403), datasource not found, empty results,
  query timeouts, and malformed PromQL/LogQL syntax.

- [`references/query-patterns.md`](references/query-patterns.md) — Advanced
  query patterns for Prometheus and Loki datasources, including time range
  formats, label/metadata discovery workflows, output format reference, Loki
  series limits, and indexed vs structured-metadata vs parsed label rules.

- [`references/traceql-patterns.md`](references/traceql-patterns.md) — TraceQL
  query patterns for Tempo trace search, attribute scoping rules, and the
  distinction between `traces query` and `traces get`.
