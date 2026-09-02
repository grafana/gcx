## gcx datasources pinot query

Execute a PinotQL query against a StarTree Pinot datasource

### Synopsis

Execute a PinotQL query against a StarTree Pinot datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.pinot in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources pinot query [EXPR] [flags]
```

### Examples

```

  # Simple query
  gcx datasources pinot query -d UID 'SELECT count(*) FROM faro_pinot_events_v2'

  # With time range
  gcx datasources pinot query -d UID --since 7d \
    'SELECT count(*) FROM faro_pinot_events_v2 WHERE appId = 66'

  # Output as JSON
  gcx datasources pinot query -d UID 'SELECT 1 FROM faro_pinot_events_v2' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources pinot query -d UID 'SELECT 1 FROM faro_pinot_events_v2' --share-link

  # Disable limit enforcement
  gcx datasources pinot query -d UID 'SELECT * FROM faro_pinot_events_v2' --limit 0
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.pinot is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return; requests above 1000 are capped, with a warning. Not applied to UNION or OFFSET queries (warned on stderr). 0 disables enforcement (default 100)
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

* [gcx datasources pinot](gcx_datasources_pinot.md)	 - Query StarTree Pinot datasources

