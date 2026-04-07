## gcx traces metrics

Execute a TraceQL metrics query

### Synopsis

Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Range mode is the default. Use --instant to compute a single value across
selected time range. If no time range is provided, gcx queries the last hour,
matching Tempo's default window.

```
gcx traces metrics TRACEQL [flags]
```

### Examples

```

  # Range query over the last hour (default)
  gcx traces metrics '{ } | rate()'

  # Range query with explicit relative window
  gcx traces metrics -d tempo-001 '{ } | rate()' --since 1h

  # Instant query over the last hour
  gcx traces metrics '{ } | rate()' --instant

  # Range query with explicit time range and step
  gcx traces metrics '{ } | rate()' --from now-1h --to now --step 30s

  # Output as JSON
  gcx traces metrics -d tempo-001 '{ } | rate()' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for metrics
      --instant             Run an instant query over the selected time range instead of a range query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: graph, json, table, wide, yaml (default "table")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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


