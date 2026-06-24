## gcx metrics query

Execute a PromQL query against a Prometheus datasource

### Synopsis

Execute a PromQL query against a Prometheus datasource.

EXPR is the PromQL expression to evaluate, passed as a positional argument or
via --expr (familiar to promtool users).
Datasource is resolved from -d flag or datasources.prometheus in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx metrics query [EXPR] [flags]
```

### Examples

```

  # Instant query using configured default datasource
  gcx metrics query 'up{job="grafana"}'

  # Instant query at a specific time
  gcx metrics query 'rate(http_requests_total[5m])' --time 2026-01-15T10:30:00Z

  # Range query with explicit datasource UID
  gcx metrics query -d abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx metrics query 'up' --since 1h

  # Print a Grafana Explore share link for the executed query
  gcx metrics query 'up' --share-link

  # Output as JSON
  gcx metrics query -d abc123 'up' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --time string         Evaluation time for an instant query (RFC3339, Unix timestamp, or relative like 'now-5m'). Mutually exclusive with --from/--to/--since
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics

