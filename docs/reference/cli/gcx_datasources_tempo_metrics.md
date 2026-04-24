## gcx datasources tempo metrics

Execute a TraceQL metrics query

### Synopsis

Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query. Use --instant to force an instant query
even when a time range is provided. If no time flags are set, gcx queries the
last hour by default.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources tempo metrics [TRACEQL] [flags]
```

### Examples

```

  # Instant query over the last hour (default, no time flags)
  gcx datasources tempo metrics '{ } | rate()'

  # Range query with relative window
  gcx datasources tempo metrics -d tempo-001 '{ } | rate()' --since 1h

  # Print a Grafana Explore share link for the query
  gcx datasources tempo metrics '{ } | rate()' --share-link

  # Instant query with explicit time range
  gcx datasources tempo metrics '{ } | rate()' --instant --since 1h

  # Range query with explicit time range and step
  gcx datasources tempo metrics '{ } | rate()' --from now-1h --to now --step 30s

  # Output as JSON
  gcx datasources tempo metrics -d tempo-001 '{ } | rate()' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for metrics
      --instant             Run an instant query over the selected time range instead of a range query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: graph, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
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

* [gcx datasources tempo](gcx_datasources_tempo.md)	 - Query Tempo datasources

