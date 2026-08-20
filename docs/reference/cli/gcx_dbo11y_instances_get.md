## gcx dbo11y instances get

Inspect a single Database Observability instance: health, connections, wait events, and top queries.

### Synopsis

Show exporter health and a query-performance snapshot for one database instance.

The argument is the instance's service_name (the identifier "gcx dbo11y
instances list" reports as NAME). What's available depends on the instance's
engine (from "gcx dbo11y instances list"):

  - Health (up/down) is engine-agnostic, from the standard Prometheus scrape
    target gauge.
  - Postgres additionally reports exporter self-scrape health, per-state
    connection counts, active wait events, and the longest running
    transaction, all from postgres_exporter's pg_stat_activity collector.
  - MySQL reports a single current-connections count
    (mysql_global_status_threads_connected); wait events and exporter
    self-scrape health have no confirmed live metric for MySQL and are
    omitted rather than guessed.
  - Top queries (both engines) are ranked by time share (seconds of database
    time spent per second) over --since (default 5m): pg_stat_statements for
    Postgres, mysqld_exporter's Performance Schema eventsstatements collector
    for MySQL.

--filter only scopes the connections/query-performance queries — health and
inventory metadata don't carry a datname/schema label and are unaffected.

When no telemetry is found for the instance, this command checks whether
Database Observability has been activated for the stack and, if not, exits
non-zero with a hint to activate it in Grafana Cloud instead of a generic
"no telemetry" message.

```
gcx dbo11y instances get <name> [flags]
```

### Examples

```

  # Health, connections, and top queries for one instance (Postgres or MySQL)
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
      --filter stringArray   Scope the connections/query-performance queries to series matching a label matcher, e.g. --filter datname=payments for Postgres or --filter schema=payments for MySQL (repeatable). Does not affect health/inventory metrics, which don't carry that label
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

