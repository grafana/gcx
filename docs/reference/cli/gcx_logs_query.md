## gcx logs query

Execute a LogQL query against a Loki datasource

### Synopsis

Execute a LogQL query against a Loki datasource.

EXPR is the LogQL expression to evaluate.
Datasource is resolved from -d flag or datasources.loki in your context.

Default table output is optimized for humans. Use -o raw for original line
bodies or -o json for the full structured response.

When --limit is omitted, table/wide use 50 lines by default; other formats
keep the legacy 1000-line default.

```
gcx logs query EXPR [flags]
```

### Examples

```

  # Query logs using configured default datasource
  gcx logs query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx logs query -d abc123 '{job="varlogs"} |= "error"'

  # Raw line bodies only
  gcx logs query -d abc123 '{job="varlogs"}' -o raw

  # Output as JSON
  gcx logs query -d abc123 '{job="varlogs"}' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum number of log lines to return (0 means no limit). If omitted, table/wide use 50 and other formats use 1000 (default 1000)
  -o, --output string       Output format. One of: json, raw, table, wide, yaml (default "table")
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

* [gcx logs](gcx_logs.md)	 - Query Loki datasources and manage Adaptive Logs

