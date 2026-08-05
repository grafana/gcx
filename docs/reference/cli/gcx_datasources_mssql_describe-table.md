## gcx datasources mssql describe-table

Describe a MSSQL table

### Synopsis

List the columns of the specified table from INFORMATION_SCHEMA.COLUMNS,
reporting name, data type, nullability, max length, and default. Disambiguate a
table that exists in multiple schemas with a schema-qualified name
(SCHEMA.TABLE) or the --schema flag.

```
gcx datasources mssql describe-table TABLE [flags]
```

### Examples

```

  # Describe a table
  gcx datasources mssql describe-table WORLD_DATA

  # Restrict to a schema (equivalent forms)
  gcx datasources mssql describe-table dbo.WORLD_DATA
  gcx datasources mssql describe-table WORLD_DATA --schema dbo

  # Output as JSON
  gcx datasources mssql describe-table WORLD_DATA -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.mssql is configured)
  -h, --help                help for describe-table
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --schema string       Schema the table belongs to (e.g. dbo)
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

* [gcx datasources mssql](gcx_datasources_mssql.md)	 - Query Microsoft SQL Server datasources

