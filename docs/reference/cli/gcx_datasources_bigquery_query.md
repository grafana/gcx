## gcx datasources bigquery query

Execute a SQL query against a BigQuery datasource

### Synopsis

Execute a GoogleSQL query against a BigQuery datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.bigquery in your context.
Reference tables as `project.dataset.table`; when the project is omitted
the datasource's default project is used.
Server-side macros ($__timeFilter, etc.) are supported against TIMESTAMP
columns; a DATETIME column needs an explicit CAST(col AS TIMESTAMP) first,
since $__timeFilter substitutes a TIMESTAMP literal BigQuery won't compare
against DATETIME directly.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources bigquery query [EXPR] [flags]
```

### Examples

```

  # Simple query
  gcx datasources bigquery query 'SELECT count(*) FROM `my-project.my_dataset.events`'

  # Explicit datasource
  gcx datasources bigquery query -d UID 'SELECT * FROM `my_dataset.logs`' --since 1h

  # $__timeFilter against a TIMESTAMP column
  gcx datasources bigquery query -d UID 'SELECT * FROM `my_dataset.events` WHERE $__timeFilter(event_ts)' --since 1h

  # Output as JSON
  gcx datasources bigquery query -d UID 'SELECT 1' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources bigquery query 'SELECT 1' --share-link

  # Disable limit enforcement
  gcx datasources bigquery query 'SELECT * FROM `my_dataset.big_table`' --limit 0
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.bigquery is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int           Max rows to return; requests above 1000 are capped, with a warning (0 disables enforcement) (default 100)
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources bigquery](gcx_datasources_bigquery.md)	 - Query BigQuery datasources

