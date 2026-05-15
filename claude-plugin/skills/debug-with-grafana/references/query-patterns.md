# Query Patterns

Advanced patterns for querying Prometheus and Loki datasources with gcx.

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
# Set default Prometheus datasource
gcx config set contexts.mystack.default-prometheus-datasource <uid>

# Set default Loki datasource
gcx config set contexts.mystack.default-loki-datasource <uid>

# Now queries work without specifying a UID
gcx metrics query 'up'
gcx logs query '{job="varlogs"}'
```

## Prometheus Query Patterns

### Instant Queries

Query current values:

```bash
# Current uptime for all targets
gcx metrics query -d <uid> 'up'

# CPU usage by job
gcx metrics query -d <uid> 'avg by(job) (rate(cpu_usage_seconds[5m]))'

# Memory usage with threshold
gcx metrics query -d <uid> 'node_memory_MemAvailable_bytes < 1000000000'
```

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

### Aggregation Patterns

```bash
# Sum across all instances
gcx metrics query -d <uid> 'sum(http_requests_total)'

# Average by label
gcx metrics query -d <uid> 'avg by(job) (cpu_usage)'

# Top 5 by value
gcx metrics query -d <uid> 'topk(5, http_requests_total)'

# 95th percentile
gcx metrics query -d <uid> 'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))'
```

### Combining with Graph

```bash
# Line chart (default) — pass -o graph directly to the query command
gcx metrics query -d <uid> 'rate(http_requests_total[5m])' \
  --from now-1h --to now --step 1m -o graph

# Instant query as graph
gcx metrics query -d <uid> 'up' -o graph

# Range query as graph
gcx metrics query -d <uid> 'cpu_usage' --from now-6h --to now --step 5m -o graph
```

## Loki Query Patterns

### Log Stream Selectors

Basic log filtering:

```bash
# All logs from a job
gcx logs query -d <loki-uid> '{job="varlogs"}'

# Multiple labels (AND)
gcx logs query -d <loki-uid> '{job="varlogs",level="error"}'

# Regex matching
gcx logs query -d <loki-uid> '{job=~"mysql.*",level!="debug"}'

# Exclude specific values
gcx logs query -d <loki-uid> '{namespace="production",pod!~"test.*"}'
```

### Log Stream Operators

```bash
# Contains text
gcx logs query -d <loki-uid> '{job="varlogs"} |= "error"'

# Doesn't contain text
gcx logs query -d <loki-uid> '{job="varlogs"} != "debug"'

# Regex match in log line
gcx logs query -d <loki-uid> '{job="varlogs"} |~ "error|exception"'

# JSON parsing
gcx logs query -d <loki-uid> '{job="varlogs"} | json | level="error"'
```

### Log Range Queries

Query logs over time:

```bash
# Last hour of logs
gcx logs query -d <loki-uid> '{job="varlogs"}' \
  --from now-1h --to now

# Specific time range
gcx logs query -d <loki-uid> '{namespace="prod"}' \
  --from 2026-03-01T00:00:00Z --to 2026-03-01T12:00:00Z
```

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
```

### Combining Loki with Graph

```bash
# Visualize log volume
gcx logs query -d <loki-uid> \
  'sum(rate({job="varlogs"}[5m]))' \
  --from now-6h --to now --step 5m -o graph

# Error rate over time
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
# Output:
# CLUSTER        DETECTED_LEVEL  JOB       NAMESPACE  POD          LINE
# dev-eu-west-2  info            varlogs   prod       app-abc123   ts=2026-03-06T10:30:00Z...
# dev-eu-west-2  error           varlogs   prod       app-abc123   ts=2026-03-06T10:30:01Z...
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

### YAML Format

```bash
gcx metrics query -d <uid> 'up' -o yaml
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

## Scripting Patterns

### Automated Monitoring

```bash
#!/bin/bash
DS_UID=$(gcx datasources list --type prometheus -o json 2>/dev/null | \
  python3 -c "import json,sys; print(json.load(sys.stdin)['datasources'][0]['uid'])")

# Check if service is up
UP=$(gcx metrics query -d $DS_UID 'up{job="critical-service"}' -o json 2>/dev/null | \
     python3 -c "import json,sys; print(json.load(sys.stdin)['data']['result'][0]['value'][1])")

if [ "$UP" != "1" ]; then
  echo "ALERT: critical-service is down!"
  exit 1
fi
```

### Batch Queries

```bash
#!/bin/bash
DS_UID="<your-datasource-uid>"

QUERIES=(
  "up"
  "rate(http_requests_total[5m])"
  "node_memory_MemAvailable_bytes"
)

for query in "${QUERIES[@]}"; do
  echo "Query: $query"
  gcx metrics query -d $DS_UID "$query" --from now-5m --to now -o graph
  echo "---"
done
```

### Exporting Data

```bash
# Export query results to file
gcx metrics query -d <uid> 'cpu_usage' --from now-24h --to now --step 1m -o json > cpu-data.json

# Convert to CSV
gcx metrics query -d <uid> 'up' -o json 2>/dev/null | \
  python3 -c "import json,sys,csv; w=csv.writer(sys.stdout); data=json.load(sys.stdin); [w.writerow([r['metric'].get('job',''),r['value'][0],r['value'][1]]) for r in data['data']['result']]" > results.csv
```

## Performance Tips

### Query Optimization

1. **Use specific label filters**: More specific = faster queries
```bash
# Slow
gcx metrics query -d <uid> 'http_requests_total'

# Fast
gcx metrics query -d <uid> 'http_requests_total{job="api",status="200"}'
```

2. **Choose appropriate range selectors**:
```bash
# For rate queries, match range to step
gcx metrics query -d <uid> 'rate(requests[5m])' --step 5m

# Don't use huge ranges for instant queries
gcx metrics query -d <uid> 'rate(requests[5m])'  # Good
gcx metrics query -d <uid> 'rate(requests[1h])'  # Usually unnecessary
```

3. **Limit time ranges**:
```bash
# Query only what you need
gcx metrics query -d <uid> 'up' --from now-1h --to now  # Good
gcx metrics query -d <uid> 'up' --from now-30d --to now  # Slow
```

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
- Assuming a field visible in Grafana Explore is a stream label — check with `gcx logs labels -d <uid>` (only shows stream labels)

## Common Patterns

### Health Check

```bash
# Check if services are up
gcx metrics query -d <uid> 'up{job="critical-service"}' | grep "1"
```

### Error Rate

```bash
# HTTP error rate
gcx metrics query -d <uid> 'rate(http_requests_total{status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o graph
```

### Resource Usage

```bash
# Memory usage by pod
gcx metrics query -d <uid> 'container_memory_usage_bytes{namespace="production"}' -o graph
```

### Log Analysis

```bash
# Count errors in last hour
gcx logs query -d <loki-uid> \
  'count_over_time({job="app"} |= "error" [1h])'
```

### Comparison Queries

```bash
# Compare current vs 24h ago
gcx metrics query -d <uid> 'rate(requests[5m])' --from now-1h --to now -o json > now.json
gcx metrics query -d <uid> 'rate(requests[5m])' --from now-25h --to now-24h -o json > yesterday.json
```

## Tempo / TraceQL Patterns

For TraceQL query syntax, `traces query` vs `traces get`, attribute scoping
rules, and common trace search patterns, see
[`traceql-patterns.md`](traceql-patterns.md).
