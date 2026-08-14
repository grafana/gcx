## gcx dbo11y instances get

Inspect a single Database Observability instance: health, connections, wait events, and top queries.

### Synopsis

Show exporter health and a query-performance snapshot for one database instance.

The argument is the instance's service_name (the identifier "gcx dbo11y
instances list" reports as NAME). Health comes from pg_up and the exporter's
own scrape metrics; connections and wait events come from pg_stat_activity;
top queries are ranked by time share (seconds of database time spent per
second) from pg_stat_statements over --since (default 5m).

--filter only scopes the pg_stat_activity/pg_stat_statements queries
(connections, wait events, longest transaction, top queries) — health and
inventory metadata (pg_up, exporter scrape stats, instance metadata) don't
carry a datname label and are unaffected.

```
gcx dbo11y instances get <name> [flags]
```

### Examples

```

  # Health, connections, wait events, and top queries for one instance
  gcx dbo11y instances get quickpizza-db

  # Widen the query window and show more top queries
  gcx dbo11y instances get quickpizza-db --since 1h --top 20

  # Scope connections/queries to a single database on a multi-database instance
  gcx dbo11y instances get quickpizza-db --filter datname=payments

  # JSON for scripting
  gcx dbo11y instances get quickpizza-db -o json
```

### Options

```
  -d, --datasource string    Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --filter stringArray   Scope the pg_stat_activity/pg_stat_statements queries (connections, wait events, longest transaction, top queries) to series matching a label matcher, e.g. --filter datname=payments (repeatable). Does not affect health/inventory metrics (pg_up, exporter scrape stats, instance metadata), which don't carry a datname label
  -h, --help                 help for get
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string         Rate window applied to pg_stat_statements (e.g. 1m, 5m, 1h) — PromQL duration syntax (default "5m")
      --top int              Limit the number of top queries returned, ranked by time share (0 = unlimited) (default 10)
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

* [gcx dbo11y instances](gcx_dbo11y_instances.md)	 - Inspect Database Observability instances discovered from telemetry

