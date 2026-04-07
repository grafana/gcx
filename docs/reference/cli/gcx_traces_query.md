## gcx traces query

Search for traces using a TraceQL query

### Synopsis

Search for traces using a TraceQL query against a Tempo datasource.

TRACEQL is the TraceQL expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

```
gcx traces query TRACEQL [flags]
```

### Examples

```

  # Search traces using configured default datasource
  gcx traces query '{ span.http.status_code >= 500 }'

  # Search with explicit datasource UID and time range
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' --since 1h

  # Using the search alias
  gcx traces search '{ span.http.status_code >= 500 }' --since 1h

  # With custom limit
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' --since 1h --limit 50

  # Output as JSON
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum number of traces to return (0 means no limit) (default 20)
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

