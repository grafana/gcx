## gcx traces metrics

Execute a TraceQL metrics query

### Synopsis

Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query.

```
gcx traces metrics TRACEQL [flags]
```

### Examples

```

  # Instant query (no time flags)
  gcx traces metrics '{ } | rate()'

  # Range query with since
  gcx traces metrics -d tempo-001 '{ } | rate()' --since 1h

  # Range query with explicit time range
  gcx traces metrics '{ } | rate()' --from now-1h --to now --step 30s

  # Output as JSON
  gcx traces metrics -d tempo-001 '{ } | rate()' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for metrics
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
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

