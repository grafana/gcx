## gcx datasources mssql query

Execute a SQL query against an MSSQL datasource

### Synopsis

Execute a SQL query against a Microsoft SQL Server datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.mssql in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.

T-SQL has no LIMIT keyword. By default the result is capped with an injected
TOP (n) clause (see --limit); use --limit 0 to disable it, or write your own
TOP / OFFSET ... FETCH. Injection only applies to simple leading-SELECT
statements — CTEs (WITH), set operations (UNION/INTERSECT/EXCEPT), queries that
already use TOP, and OFFSET/FETCH queries are left unchanged.

Use --share-link to print the equivalent Grafana Explore URL, or --open to open
it in your browser after the query succeeds.

```
gcx datasources mssql query [EXPR] [flags]
```

### Examples

```

  # Simple query (capped at TOP (100))
  gcx datasources mssql query 'SELECT name, id FROM dbo.WORLD_DATA'

  # With time macro and explicit datasource
  gcx datasources mssql query -d UID 'SELECT * FROM events WHERE $__timeFilter(created_at)' --since 1h

  # Cap at 10 rows (injects TOP (10))
  gcx datasources mssql query -d UID 'SELECT * FROM dbo.WORLD_DATA' --limit 10

  # Disable TOP injection and output JSON
  gcx datasources mssql query 'SELECT * FROM dbo.WORLD_DATA' --limit 0 -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.mssql is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return via injected TOP (n) (0 disables injection) (default 100)
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
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

* [gcx datasources mssql](gcx_datasources_mssql.md)	 - Query Microsoft SQL Server datasources

