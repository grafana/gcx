## gcx datasources clickhouse describe-table

Show column schema for a ClickHouse table

### Synopsis

Show column details including name, type, default, and comment for each column in the specified table.

```
gcx datasources clickhouse describe-table TABLE [flags]
```

### Examples

```

  # Describe a table in the default database
  gcx datasources clickhouse describe-table otel_logs

  # Describe a table in a specific database
  gcx datasources clickhouse describe-table otel_logs --database otel

  # Output as JSON
  gcx datasources clickhouse describe-table otel_logs -o json
```

### Options

```
      --database string     Database name (default: "default")
  -d, --datasource string   Datasource UID (required unless datasources.clickhouse is configured)
  -h, --help                help for describe-table
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

