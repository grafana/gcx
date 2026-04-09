## gcx slo definitions timeline

Render SLI values over time as a line chart.

### Synopsis

Render SLI values over time as a line chart by executing a range query
against the Prometheus datasource associated with each SLO.

Requires that the SLO destination datasource has recording rules generating
grafana_slo_sli_window metrics.

```
gcx slo definitions timeline [UUID] [flags]
```

### Examples

```
  # Render SLI trend for all SLOs over the past 7 days.
  gcx slo definitions timeline

  # Render SLI trend for a specific SLO.
  gcx slo definitions timeline abc123def

  # Custom time range with explicit step.
  gcx slo definitions timeline --from now-24h --to now --step 5m

  # Use duration shorthand for the past 24 hours.
  gcx slo definitions timeline --since 24h

  # Output timeline data as a table.
  gcx slo definitions timeline -o table
```

### Options

```
      --from string     Start of the time range (e.g. now-7d, now-24h, RFC3339, Unix timestamp) (default "now-7d")
  -h, --help            help for timeline
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: graph, json, table, yaml (default "graph")
      --since string    Duration before now (e.g. 1h, 7d). Equivalent to --from now-<since> --to now.
      --step string     Query step (e.g. 5m, 1h). Defaults to auto-computed value.
      --to string       End of the time range (e.g. now, RFC3339, Unix timestamp) (default "now")
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

* [gcx slo definitions](gcx_slo_definitions.md)	 - Manage SLO definitions.

