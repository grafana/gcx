## gcx metrics series

List time series matching one or more selectors

### Synopsis

List time series matching one or more selectors via the Prometheus /api/v1/series endpoint.

A selector can be passed as a positional argument and/or via --match (repeatable).
Time range defaults to unbounded; pass --since, or --from/--to, to scope.

```
gcx metrics series [SELECTOR] [flags]
```

### Examples

```

  # Match a single metric family
  gcx metrics series -d <datasource-uid> '{__name__="up"}'

  # Match multiple selectors scoped to the last hour
  gcx metrics series -d <datasource-uid> --match '{job="grafana"}' --match '{job="loki"}' --since 1h

  # Output as JSON
  gcx metrics series -d <datasource-uid> '{__name__=~"grafanacloud_.*"}' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-prometheus-datasource is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for series
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --match strings       Additional series selector(s); repeatable
  -o, --output string       Output format. One of: json, table, yaml (default "table")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics

