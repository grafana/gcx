---
name: explore-datasources
description: Discover what datasources, metrics, labels, and log streams are available in a Grafana instance. Use when the user asks what data exists, what metrics are available, what services are being monitored, or needs to find a datasource UID.
---

# Datasource Discovery

> If gcx is not configured, see the setup-gcx skill first.

## Instructions

### Step 1: List Available Datasources

Start by identifying all datasources in the Grafana instance.

```bash
# List all datasources
gcx datasources list

# Filter by type if you know what you need
gcx datasources list --type prometheus
gcx datasources list --type loki
```

**Expected output:** Table showing UID, NAME, TYPE, URL, and DEFAULT columns.

**Important:** Always use the UID (not the name) in subsequent commands.

### Step 2: Explore Datasource Contents

Choose the appropriate exploration path based on datasource type.

#### For Prometheus Datasources

```bash
# List all available labels
gcx metrics labels -d <datasource-uid>

# Get values for a specific label to understand what's being monitored
gcx metrics labels -d <datasource-uid> --label job

# List all available metrics with descriptions
gcx metrics metadata -d <datasource-uid>

# Check what systems are being scraped
gcx metrics query -d <datasource-uid> 'up'
```

**Expected output:** Tables showing labels, metrics, or query results depending on command.

#### For Loki Datasources

```bash
# List all available labels
gcx logs labels -d <datasource-uid>

# Get values for a specific label
gcx logs labels -d <datasource-uid> --label job

# List log streams matching a selector (required)
gcx logs series -d <datasource-uid> -M '{job="varlogs"}'
```

**Expected output:** Tables showing labels or log stream series.

**Note:** The `series` command requires at least one `-M` (match) selector using LogQL syntax.

### Step 3: Test Queries (Optional)

Once you've identified available data, verify with a test query.

```bash
# For Prometheus - instant query
gcx metrics query <datasource-uid> 'up'

# For Prometheus - range query
gcx metrics query <datasource-uid> 'rate(http_requests_total[5m])' --from now-1h --to now
```

**Expected output:** Table showing metric values with labels and timestamps.

### Step 4: Set Default Datasource (Optional)

To avoid passing `-d <uid>` repeatedly, configure defaults:

```bash
# Set default Prometheus datasource
gcx config set contexts.<context-name>.default-prometheus-datasource <uid>

# Set default Loki datasource
gcx config set contexts.<context-name>.default-loki-datasource <uid>
```

After setting defaults, you can omit the `-d` flag in datasource commands.

## Examples

### Example 1: Finding HTTP metrics

**User says:** "What HTTP metrics are available?"

**Actions:**
1. List Prometheus datasources: `gcx datasources list --type prometheus`
2. Get datasource UID from output
3. Search for HTTP metrics: `gcx metrics metadata -d <uid> -o json | jq '.data | to_entries[] | select(.key | contains("http"))'`
4. Get details on specific metric: `gcx metrics metadata -d <uid> --metric http_requests_total`

**Result:** Metric name, type (counter/gauge), and help text showing what the metric measures.

### Example 2: Discovering which services are logging to Loki

**User says:** "What applications are sending logs to Loki?"

**Actions:**
1. List Loki datasources: `gcx datasources list --type loki`
2. Get datasource UID from output
3. List label names: `gcx logs labels -d <uid>`
4. Get job values: `gcx logs labels -d <uid> --label job`
5. List streams for a specific job: `gcx logs series -d <uid> -M '{job="varlogs"}'`

**Result:** List of job names and their associated log streams.

### Example 3: Troubleshooting missing dashboard data

**User says:** "My dashboard shows no data for service X"

**Actions:**
1. Verify datasource exists: `gcx datasources get <uid>`
2. Check if service is being monitored:
   - Prometheus: `gcx metrics query -d <uid> 'up{job="service-x"}'`
   - Look for service in scrape results
3. Verify labels exist: `gcx metrics labels -d <uid> --label job`
4. Test simple query: `gcx metrics query -d <uid> 'up{job="service-x"}'`

**Result:** Identifies whether datasource is misconfigured, service isn't being scraped, or label selectors are wrong.

### Example 4: Finding logs for a specific namespace

**User says:** "Show me all log streams from the production namespace"

**Actions:**
1. Get Loki datasource UID: `gcx datasources list --type loki`
2. Verify namespace label exists: `gcx logs labels -d <uid> --label namespace`
3. List all streams in namespace: `gcx logs series -d <uid> -M '{namespace="production"}'`

**Result:** Table showing all label combinations for log streams in the production namespace.

## Troubleshooting

### Error: "datasource UID is required"

**Cause:** The `-d` flag was omitted and no default datasource is configured.

**Solution:**
```bash
# Option 1: Pass the UID explicitly
gcx metrics labels -d <datasource-uid>

# Option 2: Set a default datasource
gcx config set contexts.<context-name>.default-prometheus-datasource <uid>
```

### Error: "at least one --match selector is required"

**Cause:** The `loki series` command was called without a `-M` flag.

**Solution:** Loki series requires at least one LogQL selector:
```bash
# Correct
gcx logs series -d <uid> -M '{job="varlogs"}'

# Wrong - will fail
gcx logs series -d <uid>
```

### Error: "parse error on line 1, column X: bare \" in non-quoted-field"

**Cause:** Shell is interpreting quotes in the LogQL selector incorrectly.

**Solution:** Use single quotes around the entire selector:
```bash
# Correct - single quotes outside
gcx logs series -d <uid> -M '{name="value", cluster="prod"}'

# Wrong - shell interprets quotes incorrectly
gcx logs series -d <uid> -M {name="value"}
```

### Error: "datasource.prometheus.datasource.grafana.app \"<uid>\" not found"

**Cause:** The datasource UID doesn't exist or you don't have access to it.

**Solution:**
1. List datasources to verify UID: `gcx datasources list`
2. Check you're using the correct context: `gcx config current-context`
3. Verify datasource exists: `gcx datasources get <uid>`

### No output from labels/series commands

**Cause:** Datasource has no data or hasn't scraped/ingested anything yet.

**Solution:**
1. For Prometheus: Check targets are active: `gcx metrics query -d <uid> 'up'`
2. For Loki: Verify labels exist: `gcx logs labels -d <uid>`
3. Check datasource URL is reachable: `gcx datasources get <uid>`

## Advanced Usage

For detailed patterns, LogQL syntax guide, and advanced discovery workflows, see:
- [`references/discovery-patterns.md`](references/discovery-patterns.md) - Common discovery patterns and workflows
- [`references/logql-syntax.md`](references/logql-syntax.md) - LogQL selector syntax guide

## Output Formats

All commands support `-o json` or `-o yaml` for programmatic use:

```bash
# Get JSON output for piping to jq
gcx metrics labels -d <uid> -o json

# Example: Count total metrics
gcx metrics metadata -d <uid> -o json | jq '.data | length'
```

Default output is `table` format for human readability.
