## gcx datasources loki query

Execute a LogQL query against a Loki datasource

### Synopsis

Execute a LogQL query against a Loki datasource.

DATASOURCE_UID is optional when datasources.loki is configured in your context.
EXPR is the LogQL expression to evaluate.

```
gcx datasources loki query [DATASOURCE_UID] EXPR [flags]
```

### Examples

```

  # Log query using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Range query with explicit datasource UID
  gcx datasources loki query loki-001 '{job="varlogs"}' --from now-1h --to now

  # With custom limit
  gcx datasources loki query loki-001 '{job="varlogs"}' --from now-1h --to now --limit 500

  # No limit (return all matching log lines)
  gcx datasources loki query loki-001 '{job="varlogs"}' --limit 0

  # Output as JSON
  gcx datasources loki query loki-001 '{job="varlogs"}' -o json
```

### Options

```
      --from string     Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help            help for query
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --limit int       Maximum number of log lines to return (0 means no limit) (default 1000)
  -o, --output string   Output format. One of: graph, json, table, wide, yaml (default "table")
      --step string     Query step (e.g., '15s', '1m')
      --to string       End time (RFC3339, Unix timestamp, or relative like 'now')
      --window string   Convenience shorthand: sets --from to now-{window} and --to to now (mutually exclusive with --from/--to)
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

* [gcx datasources loki](gcx_datasources_loki.md)	 - Loki datasource operations

