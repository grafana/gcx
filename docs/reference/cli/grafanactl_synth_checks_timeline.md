## grafanactl synth checks timeline

Render probe_success over time as a terminal line chart.

### Synopsis

Render probe_success values over time as a line chart by executing a range
query against the Prometheus datasource.

Each probe reporting for the check is rendered as a separate series.
Requires a Prometheus datasource containing SM metrics.

```
grafanactl synth checks timeline ID [flags]
```

### Examples

```
  # Render timeline for a check over the past 6 hours (default).
  grafanactl synth checks timeline 42

  # Custom time window.
  grafanactl synth checks timeline 42 --window 24h

  # Explicit time range.
  grafanactl synth checks timeline 42 --from now-24h --to now

  # Output timeline data as a table.
  grafanactl synth checks timeline 42 -o table

  # Specify the Prometheus datasource.
  grafanactl synth checks timeline 42 --datasource-uid my-prometheus
```

### Options

```
      --datasource-uid string   UID of the Prometheus datasource to query
      --from string             Start of the time range (e.g. now-6h, now-24h, RFC3339, Unix timestamp)
  -h, --help                    help for timeline
  -o, --output string           Output format. One of: graph, json, table, yaml (default "graph")
      --to string               End of the time range (e.g. now, RFC3339, Unix timestamp)
      --window string           Time window to display (e.g. 1h, 6h, 24h, 7d) (default "6h")
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

* [grafanactl synth checks](grafanactl_synth_checks.md)	 - Manage Synthetic Monitoring checks.

