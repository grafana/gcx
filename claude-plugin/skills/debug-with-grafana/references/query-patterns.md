# Query Patterns

Advanced patterns for querying Prometheus and Loki datasources with gcx.

## Contents

- [Datasource UID Resolution](#datasource-uid-resolution) - finding UIDs, setting defaults
- [Prometheus Query Patterns](#prometheus-query-patterns) - instant vs range, time formats, step intervals
- [Loki Query Patterns](#loki-query-patterns) - stream selectors, log metric queries
- [Prometheus Datasource Operations](#prometheus-datasource-operations) - label/metadata discovery workflow
- [Loki Datasource Operations](#loki-datasource-operations) - label/series discovery workflow
- [Output Formats](#output-formats) - table, wide, JSON shapes, `--json` field selection
- [Performance Tips](#performance-tips) - Loki series limits, distributed-request counting, indexed vs structured-metadata vs parsed labels
- [Comparison Queries](#comparison-queries) - comparing now vs a past instant

## Datasource UID Resolution

**CRITICAL**: Always use datasource UID, never the name.

### Finding Datasource UIDs

```bash
# List all datasources
gcx datasources list

# Filter by type
gcx datasources list --type prometheus
gcx datasources list --type loki

# Get JSON for scripting
DS_UID=$(gcx datasources list --type prometheus -o json 2>/dev/null | \
  python3 -c "import json,sys; print(json.load(sys.stdin)['datasources'][0]['uid'])")
```

### Setting Default Datasource

Avoid repeating the datasource UID argument:

```bash
# Set default Prometheus datasource (current context)
gcx config set contexts.<name>.datasources.prometheus <uid>

# Set default Loki datasource
gcx config set contexts.<name>.datasources.loki <uid>

# Now queries work without specifying a UID
gcx metrics query 'up'
gcx logs query '{job="varlogs"}'
```

## Prometheus Query Patterns

### Instant Queries

Query current values, or at a specific point in time with `--time`:

```bash
# Current uptime for all targets
gcx metrics query -d <uid> 'up'

# CPU usage by job
gcx metrics query -d <uid> 'avg by(job) (rate(cpu_usage_seconds[5m]))'

# Memory usage with threshold
gcx metrics query -d <uid> 'node_memory_MemAvailable_bytes < 1000000000'

# Instant query at a specific timestamp (mutually exclusive with --from/--to/--since)
gcx metrics query -d <uid> 'rate(http_requests_total[5m])' --time 2026-05-14T12:00:00Z
gcx metrics query -d <uid> 'up' --time now-1h
```

**Do not use `--time` with over-time aggregations** (`increase()`, `max_over_time()`, `avg_over_time()`, etc.) where the window defines the answer. Use `--from`/`--to` instead so you can see the full series and aggregate with `sum by (label)` — using `count ... > 0` on an instant result loses the magnitude.

### Range Queries

Query over time periods:

```bash
# HTTP request rate over last hour
gcx metrics query -d <uid> 'rate(http_requests_total[5m])' \
  --from now-1h --to now --step 1m

# CPU usage for specific time period
gcx metrics query -d <uid> 'avg(cpu_usage)' \
  --from 2026-03-01T00:00:00Z --to 2026-03-01T12:00:00Z --step 5m

# Disk usage over last 24 hours
gcx metrics query -d <uid> 'disk_used_percent' \
  --from now-24h --to now --step 15m
```

### Time Range Formats

gcx supports multiple time formats:

```bash
# Relative time (recommended for most cases)
--from now-1h --to now
--from now-24h --to now-1h
--from now-7d --to now

# RFC3339 timestamps
--from 2026-03-01T00:00:00Z --to 2026-03-01T12:00:00Z

# Unix timestamps
--from 1709280000 --to 1709366400

# Instant query at a specific moment (omits --from/--to/--since)
--time 2026-03-01T12:00:00Z
--time now-1h
```

### Valid vs Invalid Time Expressions

```bash
# Valid relative expressions
--from now-6h --to now
--from now-1h --to now-30m
--step 5m
--step 300s

# Valid absolute timestamp
--from 2026-03-01T00:00:00Z

# INVALID — cannot chain subtractions
--from now-6h-1m     # Use now-361m instead

# INVALID — step needs a unit suffix
--step 300           # Use --step 300s or --step 5m
```

### Step Interval

Choose step based on time range:

```bash
# Short ranges: 1-5 second steps
gcx metrics query -d <uid> 'rate(requests[1m])' \
  --from now-5m --to now --step 1s

# Medium ranges: 1-5 minute steps
gcx metrics query -d <uid> 'rate(requests[5m])' \
  --from now-6h --to now --step 1m

# Long ranges: 15-60 minute steps
gcx metrics query -d <uid> 'rate(requests[1h])' \
  --from now-7d --to now --step 1h
```

**Rule of thumb**: Step should be ~1/100th of total range for smooth charts.

Pass `-o graph` directly to any query command (instant or range) to render a
terminal line chart.

## Loki Query Patterns

### Log Stream Selectors

Basic log filtering:

```bash
# All logs from a job
gcx logs query -d <loki-uid> '{job="varlogs"}'

# Multiple labels (AND) — every key inside {} must be an INDEXED label.
# `level` works here only if it's indexed on your stack; Loki's auto-added
# `detected_level` is structured metadata, so it must go after a pipe instead:
#   {job="varlogs"} | detected_level="error"
# See "Label kinds" below.
gcx logs query -d <loki-uid> '{job="varlogs",level="error"}'

# Regex matching
gcx logs query -d <loki-uid> '{job=~"mysql.*",level!="debug"}'

# Exclude specific values
gcx logs query -d <loki-uid> '{namespace="production",pod!~"test.*"}'
```

Line filters and parsers (`|= "error"`, `|~ "regex"`, `| json`, `| logfmt`)
follow standard LogQL syntax after the selector. Time ranges use the same
`--from`/`--to` formats as Prometheus queries (see
[Time Range Formats](#time-range-formats)).

### Log Metrics (Rate Queries)

Calculate metrics from logs:

```bash
# Log rate per second
gcx logs query -d <loki-uid> \
  'rate({job="varlogs"}[5m])' \
  --from now-1h --to now --step 1m

# Sum of log rates
gcx logs query -d <loki-uid> \
  'sum(rate({namespace="production"}[5m]))' \
  --from now-6h --to now --step 5m

# Count by level
gcx logs query -d <loki-uid> \
  'sum by(level) (rate({job="varlogs"} | json [5m]))' \
  --from now-1h --to now --step 1m

# Visualize log volume with -o graph
gcx logs query -d <loki-uid> \
  'sum(rate({job="app"} |= "error" [5m]))' \
  --from now-24h --to now --step 15m -o graph
```

## Prometheus Datasource Operations

### Exploring Metrics

```bash
# List all available labels
gcx metrics labels -d <uid>

# Get values for specific label
gcx metrics labels -d <uid> --label job
gcx metrics labels -d <uid> --label instance

# Get metric metadata
gcx metrics metadata -d <uid>
gcx metrics metadata -d <uid> --metric http_requests_total

# Check scrape targets via up metric
gcx metrics query -d <uid> 'up'
```

### Discovery Workflow

1. Find interesting labels:
```bash
gcx metrics labels -d <uid>
```

2. Get values for label:
```bash
gcx metrics labels -d <uid> --label job
```

3. Query specific job:
```bash
gcx metrics query -d <uid> 'up{job="prometheus"}'
```

4. Explore available metrics for that job:
```bash
gcx metrics metadata -d <uid> | grep -i <keyword>
```

## Loki Datasource Operations

### Exploring Log Streams

```bash
# List all available labels
gcx logs labels -d <loki-uid>

# Get values for specific label
gcx logs labels -d <loki-uid> --label job
gcx logs labels -d <loki-uid> --label namespace

# Find series matching selectors
gcx logs series -d <loki-uid> -M '{job="varlogs"}'
gcx logs series -d <loki-uid> -M '{namespace="production"}' -M '{level="error"}'
```

### Discovery Workflow

1. Find available labels:
```bash
gcx logs labels -d <loki-uid>
```

2. Get values for interesting labels:
```bash
gcx logs labels -d <loki-uid> --label job
gcx logs labels -d <loki-uid> --label namespace
```

3. Find series combinations:
```bash
gcx logs series -d <loki-uid> -M '{job="varlogs"}'
```

4. Query specific stream:
```bash
gcx logs query -d <loki-uid> '{job="varlogs",namespace="prod"}'
```

## Output Formats

### Table Format (Default)

For Prometheus queries, shows metric values in a table:

```bash
gcx metrics query -d <uid> 'up'
# Output:
# METRIC    VALUE  TIMESTAMP
# up{...}   1      2026-03-03T12:00:00Z
```

For Loki queries, shows raw log lines:

```bash
gcx logs query -d <loki-uid> '{job="varlogs"}' --from now-5m --to now
# Output:
# ts=2026-03-06T10:30:00Z level=info msg="request completed" status=200
# ts=2026-03-06T10:30:01Z level=error msg="connection refused"
```

### Wide Format (Loki only)

Shows all labels plus the log line:

```bash
gcx logs query -d <loki-uid> '{job="varlogs"}' --from now-5m --to now -o wide
# Output (STREAM columns are indexed labels; detected_level is structured
# metadata surfaced in DETAILS/LEVEL, NOT a stream label you can put in {}):
# CLUSTER        JOB       NAMESPACE  POD          LEVEL  MESSAGE                  DETAILS
# dev-eu-west-2  varlogs   prod       app-abc123   info   ts=2026-03-06T10:30:00Z  detected_level=info
# dev-eu-west-2  varlogs   prod       app-abc123   error  ts=2026-03-06T10:30:01Z  detected_level=error
```

### JSON Format

Machine-readable for scripting:

```bash
gcx metrics query -d <uid> 'up' -o json
```

JSON structure:
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {"__name__": "up", "job": "prometheus"},
        "value": [1709467200, "1"]
      }
    ]
  }
}
```

### Field Selection

Use `--json` to select fields without external tools:

```bash
# Discover available fields
gcx metrics query -d <uid> 'up' --json list

# Select specific fields
gcx metrics query -d <uid> 'up' --json metric,value

# For complex filtering, pipe to python3 (jq may not be installed)
gcx metrics query -d <uid> 'up' -o json 2>/dev/null | \
  python3 -c "import json,sys; data=json.load(sys.stdin); print(len(data['data']['result']))"
```

> **Piping caution**: Never use `2>&1` when piping gcx JSON output — gcx writes
> hints to stderr that break JSON parsers. Use `2>/dev/null` instead.

## Performance Tips

### Loki Performance

1. **Use indexed labels for filtering**:
```bash
# Fast (uses indexed labels)
gcx logs query -d <loki-uid> '{job="varlogs",namespace="prod"}'

# Slow (line filter, not indexed)
gcx logs query -d <loki-uid> '{job="varlogs"} |= "namespace:prod"'
```

2. **Limit log queries**:
```bash
# The default limit is 1000 lines
# For production, consider increasing or narrowing time range
gcx logs query -d <loki-uid> '{job="varlogs"}' --from now-5m --to now
```

### Querying at Scale

Loki metric queries (`rate()`, `count_over_time()`, etc.) produce one series per unique label combination. At scale this hits series limits (default 20K). Always aggregate:

```bash
# BAD — one series per pod/namespace/level/... combination
gcx logs query -d <loki-uid> 'count_over_time({job="app"} [5m])'

# GOOD — aggregate down to what you need
gcx logs query -d <loki-uid> 'sum(count_over_time({job="app"} [5m]))'
gcx logs query -d <loki-uid> 'sum by(level) (count_over_time({job="app"} | json [5m]))'
gcx logs query -d <loki-uid> 'topk(10, sum by(pod) (rate({job="app"} [5m])))'
```

Rule of thumb: if your query uses `rate()`, `count_over_time()`, or `bytes_over_time()`, wrap it with `sum()`, `sum by(label)`, or `topk()`.

### Counting requests across distributed services

A single HTTP request typically produces one log line at *every* service it
traverses (gateway, upstream, backend). Using a label selector that matches
all of them double- or triple-counts the same request.

**Symptom**: counts look ~2-3x what the rest of the evidence (dashboards,
metrics, traces) suggests.

**Wrong** (counts every hop):
```bash
gcx logs query -d <loki-uid> \
  'sum(count_over_time({job=~".+"} | json | path="/api/<endpoint>" | status >= 500 [6h]))'
```

**Right** — scope the label selector to the *backend services that own the
request*, not the gateway / proxy / load balancer:
```bash
gcx logs query -d <loki-uid> \
  'sum(count_over_time({job=~"<backend-svc-a>|<backend-svc-b>"} | json | __error__="" | path="/api/<endpoint>" | status >= 500 [6h]))'
```

How to identify the backend services for a path:
- Look at a representative trace with `gcx traces get <id> --llm -o json` and pick
  the service where the actual error (`status: error`) originates, plus its
  immediate caller.
- Or use `gcx logs labels -d <uid> -l job` to enumerate jobs, then exclude
  obvious gateway/proxy names (anything that just forwards — e.g. `webapp`,
  `api-gateway`, `envoy`, `nginx`).

Alternative: deduplicate by `trace_id` if the logs contain it:
```bash
gcx logs query -d <loki-uid> \
  'count(count by (trace_id) (rate({job=~".+"} | json | path="/api/<endpoint>" | status >= 500 [6h])))'
```

Always include `| __error__=""` after `| json` to drop lines that failed to
parse, otherwise the parser stage silently keeps them and they pollute the
count.

### Label kinds: indexed vs structured metadata vs parsed

Loki has **three** kinds of key/value data on a log line. Confusing them causes
silent empty results — a query that returns `success` with zero streams:

| | Indexed stream labels | Structured metadata | Parsed labels |
|---|---|---|---|
| Set by | Ingestion / stream config | Attached per-line at ingest (incl. Loki's auto-added `detected_level`) | Parser stages (`\| json`, `\| logfmt`) |
| Used in | Stream selector `{job="app"}` | Filter **after** a pipe: `\| detected_level="error"` | Filter **after** the parser: `\| json \| status="500"` |
| Indexed | Yes (fast) | No | No |
| Valid inside `{}` | **Yes** | **No** | **No** |

How gcx surfaces them in `gcx logs query` output (as of the label-type split):
- `-o json`: each stream's `stream` map is **indexed labels only**; each entry
  carries `structuredMetadata` and `parsed` maps for the non-indexed keys.
- `-o table`: the `STREAM` column shows indexed labels; structured metadata and
  parsed labels land in `DETAILS` (and `detected_level` fills `LEVEL`).

So the rule is simple: **only keys that appear in `stream` (or the `STREAM`
column) are valid inside `{...}`.** Anything under `structuredMetadata` / `parsed`
/ `DETAILS` must go after a `|`.

Common mistakes:
- Putting a structured-metadata key inside `{}` — fails silently:
  `{detected_level="error"}` matches nothing; use `{job="app"} | detected_level="error"` instead.
- Putting a parsed field in `{}` before the parser runs — add `| json` / `| logfmt` first, then `| status="500"`.
- `gcx logs labels` lists the **indexed** label names (what's valid in `{}`); it
  does NOT enumerate structured-metadata keys, so don't assume a key is a stream
  label just because you saw it in query output — check whether it came from
  `stream` vs `structuredMetadata`.

## Comparison Queries

```bash
# Compare current vs 24h ago — use --time for instant snapshots, not range queries
gcx metrics query -d <uid> 'rate(requests[5m])' --time now -o json > now.json
gcx metrics query -d <uid> 'rate(requests[5m])' --time now-24h -o json > yesterday.json
```
