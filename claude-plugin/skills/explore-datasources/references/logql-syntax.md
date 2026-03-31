# LogQL Selector Syntax Guide

This guide covers LogQL stream selector syntax used with the `gcx datasources loki series` command.

## Overview

The `series` command requires at least one `--match` (`-M`) selector using LogQL stream selector syntax. This is different from full LogQL queries - it only supports label matching, not log filtering or parsing.

## Basic Syntax

LogQL selectors use curly braces with label matchers:

```
{label="value"}
```

Multiple labels use comma separation (AND logic):

```
{label1="value1", label2="value2"}
```

## Operators

### Exact Match: `=`

Match label with exact value:

```bash
# Exact match
gcx datasources loki series -d <uid> -M '{job="varlogs"}'

# Multiple exact matches (AND)
gcx datasources loki series -d <uid> -M '{job="varlogs", namespace="default"}'
```

### Not Equal: `!=`

Exclude label with specific value:

```bash
# Exclude specific job
gcx datasources loki series -d <uid> -M '{job!="system"}'

# Combine with exact match
gcx datasources loki series -d <uid> -M '{namespace="production", job!="debug"}'
```

### Regex Match: `=~`

Match label with regular expression:

```bash
# Match jobs starting with "app"
gcx datasources loki series -d <uid> -M '{job=~"app.*"}'

# Match multiple patterns
gcx datasources loki series -d <uid> -M '{container_name=~"prometheus.*|grafana.*"}'

# Match namespace pattern
gcx datasources loki series -d <uid> -M '{namespace=~"kube-.*"}'
```

### Regex Not Match: `!~`

Exclude labels matching a regular expression:

```bash
# Exclude test namespaces
gcx datasources loki series -d <uid> -M '{namespace!~".*-test"}'

# Exclude debug and temp jobs
gcx datasources loki series -d <uid> -M '{job!~"debug|temp"}'
```

## Common Patterns

### Match Single Label

```bash
# Find all streams with specific job
gcx datasources loki series -d <uid> -M '{job="varlogs"}'

# Find all streams in namespace
gcx datasources loki series -d <uid> -M '{namespace="production"}'
```

### Match Multiple Labels (AND)

All labels must match:

```bash
# Job AND namespace
gcx datasources loki series -d <uid> -M '{job="varlogs", namespace="default"}'

# Three labels
gcx datasources loki series -d <uid> -M '{job="api", namespace="production", environment="prod"}'
```

### Match Multiple Selectors (OR)

Use multiple `-M` flags for OR logic:

```bash
# Job A OR Job B
gcx datasources loki series -d <uid> -M '{job="varlogs"}' -M '{job="systemlogs"}'

# Different namespaces
gcx datasources loki series -d <uid> -M '{namespace="prod"}' -M '{namespace="staging"}'
```

### Regex for Multiple Values

```bash
# Match multiple jobs with regex
gcx datasources loki series -d <uid> -M '{job=~"app-.*"}'

# Match containers with prefix
gcx datasources loki series -d <uid> -M '{container_name=~"prometheus.*", component="server"}'
```

### Exclude Patterns

```bash
# Exclude test environments
gcx datasources loki series -d <uid> -M '{namespace!~".*-test"}'

# Production only, exclude debug
gcx datasources loki series -d <uid> -M '{environment="production", job!="debug"}'
```

## Real-World Examples

### Find Application Logs

```bash
# All logs for myapp
gcx datasources loki series -d <uid> -M '{app="myapp"}'

# Myapp in production
gcx datasources loki series -d <uid> -M '{app="myapp", environment="production"}'

# Myapp, exclude debug logs
gcx datasources loki series -d <uid> -M '{app="myapp", level!="debug"}'
```

### Find Container Logs

```bash
# Specific container
gcx datasources loki series -d <uid> -M '{container_name="nginx"}'

# All containers in pod
gcx datasources loki series -d <uid> -M '{pod_name="web-server-abc123"}'

# Containers matching pattern
gcx datasources loki series -d <uid> -M '{container_name=~"nginx.*"}'
```

### Find Kubernetes Logs

```bash
# All logs in namespace
gcx datasources loki series -d <uid> -M '{namespace="kube-system"}'

# Specific deployment
gcx datasources loki series -d <uid> -M '{namespace="default", deployment="api-server"}'

# All namespaces except system
gcx datasources loki series -d <uid> -M '{namespace!~"kube-.*"}'
```

### Find Service Logs

```bash
# Logs from specific service
gcx datasources loki series -d <uid> -M '{service="api"}'

# Service in specific cluster
gcx datasources loki series -d <uid> -M '{service="api", cluster="us-west-1"}'

# Multiple services (OR logic)
gcx datasources loki series -d <uid> -M '{service="api"}' -M '{service="worker"}'
```

## Shell Quoting

Always use single quotes around the selector to prevent shell interpretation:

```bash
# ✅ Correct - single quotes outside
gcx datasources loki series -d <uid> -M '{name="value", cluster="prod"}'

# ❌ Wrong - shell interprets quotes
gcx datasources loki series -d <uid> -M {name="value"}

# ❌ Wrong - double quotes outside cause parsing errors
gcx datasources loki series -d <uid> -M "{name='value'}"
```

## Limitations

The `series` command supports **label selectors only**, not full LogQL features:

### ✅ Supported (Label Selectors)

- Exact match: `{job="varlogs"}`
- Regex match: `{job=~"app.*"}`
- Not equal: `{job!="debug"}`
- Regex not match: `{job!~"test.*"}`
- Multiple labels: `{job="api", namespace="prod"}`

### ❌ Not Supported (Query Features)

- Log filters: `{job="varlogs"} |= "error"` (not supported)
- Parser stages: `{job="varlogs"} | json` (not supported)
- Line filters: `{job="varlogs"} |~ "error.*"` (not supported)
- Metrics: `rate({job="varlogs"}[5m])` (not supported)

For these advanced features, use `gcx datasources loki query <uid> '<logql>'` to run full LogQL queries against a Loki datasource.

## Regex Syntax

Loki uses [RE2 regex syntax](https://github.com/google/re2/wiki/Syntax). Common patterns:

```bash
# Start with
{job=~"app.*"}

# End with
{job=~".*-prod"}

# Contains
{job=~".*production.*"}

# Multiple options (OR)
{job=~"api|worker|scheduler"}

# Case insensitive (use (?i) flag)
{job=~"(?i)error.*"}

# Character class
{namespace=~"prod-[0-9]+"}
```

## Tips

1. **Start broad, then narrow**: Begin with a single label, then add more filters
   ```bash
   # Start
   gcx datasources loki series -d <uid> -M '{job="varlogs"}'

   # Then narrow
   gcx datasources loki series -d <uid> -M '{job="varlogs", namespace="prod"}'
   ```

2. **Use regex for exploration**: When you don't know exact values
   ```bash
   # Find all app-* jobs
   gcx datasources loki series -d <uid> -M '{job=~"app-.*"}'
   ```

3. **Check label values first**: Use `labels` command to see available values
   ```bash
   # See what jobs exist
   gcx datasources loki labels -d <uid> --label job

   # Then query specific job
   gcx datasources loki series -d <uid> -M '{job="<value-from-above>"}'
   ```

4. **Use JSON output for large results**: Pipe to jq for filtering
   ```bash
   gcx datasources loki series -d <uid> -M '{namespace="prod"}' -o json | jq '.data[] | select(.job=="api")'
   ```

## See Also

- [Discovery Patterns](discovery-patterns.md) - Common workflows and use cases
- [Loki LogQL Documentation](https://grafana.com/docs/loki/latest/logql/) - Official LogQL documentation (for full query syntax)
