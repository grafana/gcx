## gcx metrics billing series

List billing time series matching a selector

### Synopsis

List time series matching one or more selectors via the Prometheus /api/v1/series endpoint.

A selector can be passed as a positional argument and/or via --match (repeatable).
Time range defaults to unbounded; pass --since, or --from/--to, to scope.

```
gcx metrics billing series [SELECTOR] [flags]
```

### Examples

```

  # All billing series
  gcx metrics billing series '{__name__=~"grafanacloud_.*"}' --since 1h

  # Filter to a specific product
  gcx metrics billing series '{__name__=~"grafanacloud_.*",product="metrics"}' --since 1h

  # Output as JSON
  gcx metrics billing series '{__name__=~"grafanacloud_.*"}' --since 1h -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for series
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --match stringArray   Additional series selector(s); repeatable
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

* [gcx metrics billing](gcx_metrics_billing.md)	 - Query Grafana Cloud billing metrics (grafanacloud_*)

