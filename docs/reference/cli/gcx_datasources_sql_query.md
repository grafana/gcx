## gcx datasources sql query

Execute a SQL query against the dsabstraction API

### Synopsis

Execute a SQL query that can reference one or more datasources via
the dsabstraction.grafana.app API.

The SQL string can be passed as a positional argument, via --query, via
--query-file, or piped on stdin. Datasources are referenced inside the FROM
clause as `<type>::<uid>`.`<table>`, e.g.
`prometheus::abc123`.`up`.

Requires a Grafana that exposes the dsabstraction.grafana.app/v1alpha1 API.

```
gcx datasources sql query [SQL] [flags]
```

### Examples

```

  # Inline SQL with a relative time range
  gcx datasources sql query 'SELECT timestamp, value, job FROM `prometheus::UID`.`up` LIMIT 10' --from now-5m --to now

  # Read SQL from a file
  gcx datasources sql query --query-file query.sql --since 1h

  # Pipe SQL on stdin
  echo 'SELECT 1' | gcx datasources sql query --from now-5m --to now

  # Disable server-side pushdown (for A/B comparison)
  gcx datasources sql query 'SELECT job, SUM(value) FROM `prometheus::UID`.`up` GROUP BY job' \
      --from now-5m --to now --pushdown=false

  # Show the pushdown plan that the server reports
  gcx datasources sql query 'SELECT 1' --from now-5m --to now --show-plan
```

### Options

```
      --config string       Path to the configuration file to use
      --context string      Name of the context to use
      --cookie string       Literal Cookie header to attach to the request (e.g. 'grafana_session=abc123'); intended for local dev where the apiserver expects cookie auth
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --pushdown string     Override server-side pushdown ('true' or 'false'); leave unset for server default
      --query string        SQL query (alternative to positional argument or stdin)
      --query-file string   Path to a file containing the SQL query
      --show-plan           Print the pushdown plan reported by the server before the result
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources sql](gcx_datasources_sql.md)	 - Cross-datasource SQL via the dsabstraction.grafana.app API

