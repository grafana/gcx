## gcx datasources mysql list-tables

List tables from a MySQL datasource

### Synopsis

List tables and views from all non-system databases, or filter to a specific database.

Shows database, name, and type for each table.

```
gcx datasources mysql list-tables [flags]
```

### Examples

```

  # List all tables
  gcx datasources mysql list-tables -d UID

  # Filter to a specific database
  gcx datasources mysql list-tables --database mydb

  # Output as JSON
  gcx datasources mysql list-tables -o json
```

### Options

```
      --database string     Filter tables to this database
  -d, --datasource string   Datasource UID (required unless datasources.mysql is configured)
  -h, --help                help for list-tables
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

