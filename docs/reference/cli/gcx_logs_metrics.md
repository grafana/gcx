## gcx logs metrics

Execute a metric LogQL query against a Loki datasource

### Synopsis

Execute a metric LogQL query and return time-series results.

EXPR is a metric LogQL expression (e.g., rate, count_over_time, sum).
Datasource is resolved from -d flag or datasources.loki in your context.

Unlike 'logs query' which returns log lines, 'logs metrics' returns
time-series data with proper table, graph, and JSON formatters.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query.

```
gcx logs metrics [EXPR] [flags]
```

### Examples

```

  # Rate of log lines over 5 minutes
  gcx datasources loki metrics 'rate({job="varlogs"}[5m])' --since 1h -o table

  # Count of error logs
  gcx datasources loki metrics 'count_over_time({job="varlogs"} |= "error" [5m])' --since 1h

  # Line chart output
  gcx datasources loki metrics -d loki-001 'rate({job="varlogs"}[5m])' --since 1h -o graph

  # Output as JSON
  gcx datasources loki metrics 'rate({job="varlogs"}[5m])' --since 1h -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for metrics
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: graph, json, table, wide, yaml (default "table")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx logs](gcx_logs.md)	 - Query Loki datasources and manage Adaptive Logs

