## gcx traces metrics

Execute a TraceQL metrics query

### Synopsis

Execute a TraceQL metrics query against a Tempo datasource.

DATASOURCE_UID is optional when datasources.tempo is configured in your context.
TRACEQL is the TraceQL metrics expression to evaluate.

By default, this runs a range query. Use --instant for point-in-time queries.

```
gcx traces metrics [DATASOURCE_UID] TRACEQL [flags]
```

### Examples

```

  # Range query using configured default datasource
  gcx traces metrics '{ } | rate()'

  # Range query with explicit datasource and time range
  gcx traces metrics tempo-001 '{ } | rate()' --since 1h

  # Instant query
  gcx traces metrics tempo-001 '{ } | rate()' --instant --since 1h

  # Custom step interval
  gcx traces metrics tempo-001 '{ } | rate()' --since 1h --step 30s

  # Output as JSON
  gcx traces metrics tempo-001 '{ } | rate()' -o json
```

### Options

```
      --from string     Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help            help for metrics
      --instant         Execute an instant query instead of a range query
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

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

