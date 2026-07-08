# Datasource Discovery Patterns

Common patterns and workflows for discovering datasource contents.

## Contents

- [Pattern 1: Finding HTTP Metrics](#pattern-1-finding-http-metrics)
- [Pattern 2: Understanding Service Labels](#pattern-2-understanding-service-labels)
- [Pattern 3: Discovering Available Services](#pattern-3-discovering-available-services)
- [Pattern 4: Discovering Loki Log Streams](#pattern-4-discovering-loki-log-streams)
- [Saving Datasource UIDs](#saving-datasource-uids)
- [Searching Metrics with jq](#searching-metrics-with-jq)
- [Filtering by Label](#filtering-by-label)
- [Which Datasource Has Data for System X?](#which-datasource-has-data-for-system-x)
- [Limitations](#limitations)
- [Visualizing Query Results](#visualizing-query-results)

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
gcx metrics query -d <uid> 'rate(prometheus_http_requests_total[5m])'
```

## Pattern 2: Understanding Service Labels

```bash
# 1. List all labels
gcx metrics labels -d <uid>

# 2. Get all job names
gcx metrics labels -d <uid> --label job

# 3. Get all instances for a specific job
gcx metrics query -d <uid> 'up{job="my-service"}'

# 4. List available status codes
gcx metrics labels -d <uid> --label code
```

## Pattern 3: Discovering Available Services

```bash
# 1. Check active scrape targets
gcx metrics query -d <uid> 'up'

# 2. Get unique jobs
gcx metrics labels -d <uid> --label job

# 3. Find down targets
gcx metrics query -d <uid> 'up == 0'

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
  gcx metrics query -d <uid> "up{job=\"$job\"}"
done
```

## Which Datasource Has Data for System X?

**For Prometheus:**
1. List all datasources
2. For each Prometheus datasource:
   - Get label values for `job`, `instance`, or `app`
   - Search for system name in labels
3. Test query to confirm: `gcx metrics query -d <uid> 'up{job="system-x"}'`

**For Loki:**
1. List all Loki datasources
2. For each Loki datasource:
   - Get label values for `job`, `namespace`, or `container_name`
   - List series matching the system: `gcx logs series -d <uid> -M '{job="system-x"}'`

## Limitations

- **Prometheus-specific operations**: the `metadata` command only works with Prometheus datasources
- **Loki-specific operations**: the `series` command requires at least one `--match` selector
- **Other datasource types**: Postgres, MySQL, etc. require different exploration methods (not covered by these commands)
- **Large datasources**: May have thousands of metrics/streams (use `-o json | jq` to filter)
- **LogQL complexity**: the `series` command supports label selectors only, not full LogQL queries with filters

## Visualizing Query Results

For Prometheus range queries, use the `-o graph` output codec to render results as a terminal graph:

```bash
# Query and render as terminal graph
gcx metrics query -d <uid> 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m -o graph
```

Note: The graph output codec only works with range queries (requires `--from`, `--to`, and `--step`).
