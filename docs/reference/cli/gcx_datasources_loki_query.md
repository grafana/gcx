## gcx datasources loki query

Execute a LogQL query against a Loki datasource

### Synopsis

Execute a LogQL query against a Loki datasource.

EXPR is the LogQL expression to evaluate.
Datasource is resolved from -d flag or datasources.loki in your context.

Default table output is optimized for humans. Use -o raw for original line
bodies or -o json for the full structured response.

Default --limit is 50; use --limit 0 for no cap.

```
gcx datasources loki query [EXPR] [flags]
```

### Examples

```

  # Query logs using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx datasources loki query -d UID '{job="varlogs"} |= "error"'

  # Raw line bodies only
  gcx datasources loki query -d UID '{job="varlogs"}' -o raw

  # Output as JSON
  gcx datasources loki query -d UID '{job="varlogs"}' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum number of log lines to return (0 means no limit) (default 50)
  -o, --output string       Output format. One of: json, raw, table, wide, yaml (default "table")
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

* [gcx datasources loki](gcx_datasources_loki.md)	 - Query Loki datasources

