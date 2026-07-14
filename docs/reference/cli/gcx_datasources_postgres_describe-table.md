## gcx datasources postgres describe-table

Show the columns of a PostgreSQL table

### Synopsis

Show the columns of a PostgreSQL table: name, data type, nullability, and default.

Use --schema to disambiguate when the same table name exists in multiple schemas.

```
gcx datasources postgres describe-table TABLE [flags]
```

### Examples

```

  # Describe a table
  gcx datasources postgres describe-table orders

  # Disambiguate by schema
  gcx datasources postgres describe-table orders --schema public

  # Output as JSON
  gcx datasources postgres describe-table orders -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.postgres is configured)
  -h, --help                help for describe-table
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --schema string       Schema of the table (defaults to all schemas)
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

