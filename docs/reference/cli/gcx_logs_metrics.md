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
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

Before executing, a pre-flight index-stats check estimates the bytes this
query would scan and prints a non-blocking warning if it exceeds
--stats (default 1GiB). Use --skip-stats to disable this check.
Only the query's stream selector is used for the estimate, since Loki's index
tracks streams, not line filters or parsing stages. The checked window is
widened by any range-vector duration or offset in EXPR (e.g. '[24h]',
'offset 1h'), since Loki evaluates further back than the query's own time
range alone would suggest.

```
gcx logs metrics [EXPR] [flags]
```

### Examples

```

  # Run a metric query over logs
  gcx logs metrics -d UID 'rate({job="grafana"}[5m])' --since 1h

  # Print a Grafana Explore share link for the query
  gcx logs metrics 'rate({job="grafana"}[5m])' --share-link

  # Output as JSON
  gcx logs metrics -d UID 'rate({job="grafana"}[5m])' --since 1h -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for metrics
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --skip-stats          Skip the index-stats pre-flight check
      --stats string        Warn (non-blocking) if index-stats reports more than this many bytes would be scanned (e.g. '500MiB', '2GiB') (default "1GiB")
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx logs](gcx_logs.md)	 - Query Loki datasources and manage Adaptive Logs

