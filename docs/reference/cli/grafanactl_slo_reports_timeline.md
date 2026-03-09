## grafanactl slo reports timeline

Render SLI values over time for SLO reports.

### Synopsis

Render SLI values over time as line charts for each SLO report by
executing range queries against the Prometheus datasource associated with
each constituent SLO.

Requires that SLO destination datasources have recording rules generating
grafana_slo_sli_window metrics.

```
grafanactl slo reports timeline [UUID] [flags]
```

### Examples

```
  # Render SLI trend for all SLO reports over the past 7 days.
  grafanactl slo reports timeline

  # Render SLI trend for a specific report.
  grafanactl slo reports timeline abc123def

  # Custom time range with explicit step.
  grafanactl slo reports timeline --from now-24h --to now --step 5m

  # Use window shorthand for the past 24 hours.
  grafanactl slo reports timeline --window 24h

  # Output timeline data as a table.
  grafanactl slo reports timeline -o table
```

### Options

```
      --from string     Start of the time range (e.g. now-7d, now-24h, RFC3339, Unix timestamp) (default "now-7d")
  -h, --help            help for timeline
  -o, --output string   Output format. One of: graph, json, table, yaml (default "graph")
      --step string     Query step (e.g. 5m, 1h). Defaults to auto-computed value.
      --to string       End of the time range (e.g. now, RFC3339, Unix timestamp) (default "now")
      --window string   Time window shorthand (e.g. 1h, 7d). Equivalent to --from now-<window> --to now.
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl slo reports](grafanactl_slo_reports.md)	 - Manage SLO reports.

