## gcx frontend query

Run one PinotQL statement against the Frontend Observability tables.

### Synopsis

Run one PinotQL statement against the Frontend Observability Pinot tables.

This is the escape hatch for questions the named commands (errors, pages) do
not answer. Start from a statement printed by --sql on one of them.

Guard rails, checked before the request is sent:
  - The statement must read from faro_pinot_events_v1, faro_pinot_events_v2,
    faro_pinot_exceptions_v1, faro_pinot_measurements_v1, or
    faro_exception_groups.
  - With --app, the SQL must contain appId = <id>; the command refuses rather
    than rewriting your SQL. Without --app, the query is unscoped and a
    warning is printed.

Put AND $__timeFilter("timestamp") in the WHERE clause: the datasource expands
it from --since/--from/--to (default 24h, at most 30 days). Without it, or
ago(...), the time flags have no effect and the statement scans the full
retention; a warning says so. --limit follows gcx datasources pinot query:
default 100, capped at 1000, with the same stderr notices.

--multistage runs the statement on Pinot's multi-stage engine by prefixing
SET useMultistageEngine = true;. Use it only for JOIN, WITH, UNION or window
functions.

```
gcx frontend query [SQL] [flags]
```

### Examples

```
  # Error count per type for one app
  gcx frontend query --app 187 'SELECT exceptionType, count(*) AS n FROM faro_pinot_exceptions_v1 WHERE appId = 187 AND $__timeFilter("timestamp") GROUP BY exceptionType ORDER BY n DESC'

  # Multi-stage join over a week
  gcx frontend query --app 187 --since 7d --multistage --expr 'WITH e AS (SELECT sessionId FROM faro_pinot_exceptions_v1 WHERE appId = 187 AND $__timeFilter("timestamp")) SELECT count(*) FROM e'
```

### Options

```
      --app string          Frontend Observability app: numeric ID, slug-id, or name
  -d, --datasource string   Frontend Observability Pinot datasource UID (defaults to datasources.pinot in the context, then auto-discovery)
      --expr string         PinotQL statement (alternative to the positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return; requests above 1000 are capped (stderr notice when the query is adjusted). 0 disables enforcement (default 100)
      --multistage          Run the statement on Pinot's multi-stage engine (needed for JOIN, WITH, UNION, and window functions)
      --open                Open the primary query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the primary query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --table string        StarTree table name when the SQL has no extractable FROM; must be a Frontend Observability table
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend](gcx_frontend.md)	 - Manage Grafana Frontend Observability resources

