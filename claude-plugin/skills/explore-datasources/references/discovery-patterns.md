# Datasource Discovery Patterns

This document contains common patterns and workflows for discovering datasource contents.

## Pattern 1: Finding HTTP Metrics

```bash
# 1. List datasources
gcx datasources list

# 2. Get all metrics (as JSON)
gcx metrics metadata -d <uid> -o json > metrics.json

# 3. Search for HTTP-related metrics
grep -i http metrics.json

# 4. Get metadata for specific HTTP metric
gcx metrics metadata -d <uid> --metric prometheus_http_requests_total

# 5. Test query
gcx metrics query <uid> 'rate(prometheus_http_requests_total[5m])'
```

## Pattern 2: Understanding Service Labels

```bash
# 1. List all labels
gcx metrics labels -d <uid>

# 2. Get all job names
gcx metrics labels -d <uid> --label job

# 3. Get all instances for a specific job
gcx metrics query <uid> 'up{job="my-service"}'

# 4. List available status codes
gcx metrics labels -d <uid> --label code
```

## Pattern 3: Discovering Available Services

```bash
# 1. Check active scrape targets
gcx metrics query <uid> 'up'

# 2. Get unique jobs
gcx metrics labels -d <uid> --label job

# 3. Check which services are up
gcx metrics query <uid> 'up'

# 4. Get detailed labels for a service
gcx metrics labels -d <uid> --label app
```

## Pattern 4: Discovering Loki Log Streams

```bash
# 1. List Loki datasources
gcx datasources list --type loki

# 2. List all labels
gcx logs labels -d <loki-uid>

# 3. Get all job values
gcx logs labels -d <loki-uid> --label job

# 4. Find log streams for a specific job
gcx logs series -d <loki-uid> -M '{job="varlogs"}'

# 5. Find log streams for a namespace with regex
gcx logs series -d <loki-uid> -M '{namespace=~"kube.*"}'

# 6. Multiple label filters
gcx logs series -d <loki-uid> -M '{job="varlogs", namespace="default"}'
```

## Saving Datasource UIDs

After listing datasources, save UIDs to environment variables for convenience:

```bash
# Save Prometheus UID to variable
PROM_UID=$(gcx datasources list -o json | jq -r '.datasources[] | select(.type=="prometheus") | .uid' | head -1)

# Save Loki UID to variable
LOKI_UID=$(gcx datasources list -o json | jq -r '.datasources[] | select(.type=="loki") | .uid' | head -1)

# Use in subsequent commands
gcx metrics labels -d $PROM_UID
gcx logs labels -d $LOKI_UID
```

## Searching Metrics with jq

Use jq to filter and search through large metric sets:

```bash
# Get all counter metrics
gcx metrics metadata -d <uid> -o json | jq '.data | to_entries[] | select(.value[0].type=="counter")'

# Search for metrics containing "http"
gcx metrics metadata -d <uid> -o json | jq '.data | to_entries[] | select(.key | contains("http"))'

# Count total metrics
gcx metrics metadata -d <uid> -o json | jq '.data | length'

# List all metric names
gcx metrics metadata -d <uid> -o json | jq '.data | keys[]'
```

## Filtering by Label

Combine label discovery with queries:

```bash
# Get all jobs
JOBS=$(gcx metrics labels -d <uid> --label job -o json)

# Query each job
for job in $(echo $JOBS | jq -r '.data[]'); do
  echo "Job: $job"
  gcx metrics query <uid> "up{job=\"$job\"}"
done
```

## Checking Target Health

Monitor scrape target health:

```bash
# Check active targets via up metric
gcx metrics query <uid> 'up'

# Query for down targets
gcx metrics query <uid> 'up == 0'

# List available metrics metadata
gcx metrics metadata -d <uid>
```

## Use Case Workflows

### "What data is available for my dashboard?"

1. List datasources to find relevant ones
2. For Prometheus: list labels and metadata
3. For Loki: list labels and log streams
4. Search for metrics/logs related to your domain (http, database, etc.)
5. Test queries to verify data exists

### "Which datasource has data for system X?"

**For Prometheus:**
1. List all datasources
2. For each Prometheus datasource:
   - Get label values for `job`, `instance`, or `app`
   - Search for system name in labels
3. Test query to confirm: `gcx metrics query <uid> 'up{job="system-x"}'`

**For Loki:**
1. List all Loki datasources
2. For each Loki datasource:
   - Get label values for `job`, `namespace`, or `container_name`
   - List series matching the system: `gcx logs series -d <uid> -M '{job="system-x"}'`

### "What metrics are available for HTTP requests?"

1. Get datasource UID
2. List all metrics as JSON
3. Search for "http" in metric names using grep or jq
4. Get metadata for specific HTTP metrics
5. Test queries with discovered metrics

### "Find all log streams for a specific application"

1. Get Loki datasource UID
2. List all available labels to understand structure
3. Get values for relevant labels (job, namespace, app, etc.)
4. Query series with LogQL selectors:
   ```bash
   # Find all streams for app
   gcx logs series -d <uid> -M '{app="myapp"}'

   # Find streams in specific namespace
   gcx logs series -d <uid> -M '{namespace="production"}'

   # Combine multiple criteria
   gcx logs series -d <uid> -M '{app="myapp", namespace="production"}'
   ```

## Limitations

- **Prometheus-specific operations**: metadata and targets commands only work with Prometheus datasources
- **Loki-specific operations**: series command requires at least one `--match` selector
- **Other datasource types**: Postgres, MySQL, etc. require different exploration methods (not covered by these commands)
- **Large datasources**: May have thousands of metrics/streams (use `-o json | jq` to filter)
- **Target information**: Only available if Prometheus is configured to expose it
- **LogQL complexity**: Series command supports label selectors only, not full LogQL queries with filters

## Visualizing Query Results

For Prometheus range queries, use the `-o graph` output codec to render results as a terminal graph:

```bash
# Query and render as terminal graph
gcx metrics query <uid> 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m -o graph
```

Note: The graph output codec only works with range queries (requires `--from`, `--to`, and `--step`).
