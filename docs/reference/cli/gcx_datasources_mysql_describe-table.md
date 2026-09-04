## gcx datasources mysql describe-table

Show the columns of a MySQL table

### Synopsis

Show the columns of a MySQL table: name, column type, nullability, and default.

The table can be database-qualified (db.table); otherwise use --database to
disambiguate when the same table name exists in multiple databases. TABLE and
--database both match exactly and are case-sensitive, which can differ from
how information_schema itself compares names depending on the server's
platform and lower_case_table_names setting.

```
gcx datasources mysql describe-table TABLE [flags]
```

### Examples

```

  # Describe a table
  gcx datasources mysql describe-table orders -d UID

  # Disambiguate by database
  gcx datasources mysql describe-table mydb.orders
  gcx datasources mysql describe-table orders --database mydb

  # Output as JSON
  gcx datasources mysql describe-table orders -o json
```

### Options

```
      --database string     Database of the table (exact match, case-sensitive; defaults to all databases)
  -d, --datasource string   Datasource UID (required unless datasources.mysql is configured)
  -h, --help                help for describe-table
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources mysql](gcx_datasources_mysql.md)	 - Query MySQL datasources

