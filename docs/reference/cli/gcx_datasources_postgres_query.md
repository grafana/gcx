## gcx datasources postgres query

Execute a SQL query against a PostgreSQL datasource

### Synopsis

Execute a SQL query against a PostgreSQL datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.postgres in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.

```
gcx datasources postgres query [EXPR] [flags]
```

### Examples

```

  # Simple query
  gcx datasources postgres query 'SELECT count(*) FROM orders'

  # With time macro and explicit datasource
  gcx datasources postgres query -d UID 'SELECT * FROM events WHERE $__timeFilter(created_at)' --since 1h

  # Output as JSON
  gcx datasources postgres query -d UID 'SELECT 1' -o json

  # Disable limit enforcement
  gcx datasources postgres query 'SELECT * FROM big_table' --limit 0
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.postgres is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return (0 disables enforcement) (default 100)
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources postgres](gcx_datasources_postgres.md)	 - Query PostgreSQL datasources

