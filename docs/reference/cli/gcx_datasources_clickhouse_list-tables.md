## gcx datasources clickhouse list-tables

List tables from a ClickHouse datasource

### Synopsis

List tables from all non-system databases, or filter to a specific database.

Shows database, name, engine, total_rows, and total_bytes for each table.

```
gcx datasources clickhouse list-tables [flags]
```

### Examples

```

  # List all tables
  gcx datasources clickhouse list-tables

  # Filter to a specific database
  gcx datasources clickhouse list-tables --database otel

  # Output as JSON
  gcx datasources clickhouse list-tables -o json
```

### Options

```
      --database string     Filter tables to this database
  -d, --datasource string   Datasource UID (required unless datasources.clickhouse is configured)
  -h, --help                help for list-tables
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx datasources clickhouse](gcx_datasources_clickhouse.md)	 - Query ClickHouse datasources

