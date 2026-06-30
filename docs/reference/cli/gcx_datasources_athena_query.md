## gcx datasources athena query

Execute a SQL query against an Athena datasource

### Synopsis

Execute a SQL query against an Amazon Athena datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.athena in your context.
Server-side macros ($__timeFilter, $__dateFilter, etc.) are supported.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources athena query [EXPR] [flags]
```

### Examples

```

  # Simple query
  gcx datasources athena query 'SELECT count(*) FROM events'

  # With time macro and explicit datasource
  gcx datasources athena query -d UID 'SELECT * FROM logs WHERE $__timeFilter(event_time)' --since 1h

  # With connection overrides
  gcx datasources athena query -d UID 'SELECT 1' --region us-west-2 --database analytics

  # Enable result reuse (Athena engine v3)
  gcx datasources athena query -d UID 'SELECT count(*) FROM events' --result-reuse --ttl-minutes 60

  # Disable limit enforcement
  gcx datasources athena query 'SELECT * FROM big_table' --limit 0
```

### Options

```
      --catalog string      Data catalog override
      --database string     Database override
  -d, --datasource string   Datasource UID (required unless datasources.athena is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return (0 disables enforcement) (default 100)
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --region string       AWS region override
      --result-reuse        Enable Athena query result reuse (engine v3)
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
      --ttl-minutes int     Cache TTL in minutes for result reuse; 0 disables (default 60)
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

* [gcx datasources athena](gcx_datasources_athena.md)	 - Query Amazon Athena datasources

