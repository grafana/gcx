## gcx metrics query

Execute a PromQL query against a Prometheus datasource

### Synopsis

Execute a PromQL query against a Prometheus datasource.

DATASOURCE_UID is optional when datasources.prometheus is configured in your context.
EXPR is the PromQL expression to evaluate.

```
gcx metrics query [DATASOURCE_UID] EXPR [flags]
```

### Examples

```

  # Instant query using configured default datasource
  gcx metrics query 'up{job="grafana"}'

  # Range query with explicit datasource UID
  gcx metrics query abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx metrics query abc123 'up' --since 1h

  # Output as JSON
  gcx metrics query abc123 'up' -o json
```

### Options

```
      --from string     Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help            help for query
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: graph, json, table, wide, yaml (default "table")
      --since string    Duration before --to (or now if omitted); mutually exclusive with --from
      --step string     Query step (e.g., '15s', '1m')
      --to string       End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics

