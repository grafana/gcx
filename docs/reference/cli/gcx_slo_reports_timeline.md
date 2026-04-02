## gcx slo reports timeline

Render SLI values over time for SLO reports.

### Synopsis

Render SLI values over time as line charts for each SLO report by
executing range queries against the Prometheus datasource associated with
each constituent SLO.

Requires that SLO destination datasources have recording rules generating
grafana_slo_sli_window metrics.

```
gcx slo reports timeline [UUID] [flags]
```

### Examples

```
  # Render SLI trend for all SLO reports over the past 7 days.
  gcx slo reports timeline

  # Render SLI trend for a specific report.
  gcx slo reports timeline abc123def

  # Custom time range with explicit step.
  gcx slo reports timeline --from now-24h --to now --step 5m

  # Use duration shorthand for the past 24 hours.
  gcx slo reports timeline --since 24h

  # Output timeline data as a table.
  gcx slo reports timeline -o table
```

### Options

```
      --from string     Start of the time range (e.g. now-7d, now-24h, RFC3339, Unix timestamp) (default "now-7d")
  -h, --help            help for timeline
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: graph, json, table, yaml (default "graph")
      --since string    Duration before now (e.g. 1h, 7d). Equivalent to --from now-<since> --to now.
      --step string     Query step (e.g. 5m, 1h). Defaults to auto-computed value.
      --to string       End of the time range (e.g. now, RFC3339, Unix timestamp) (default "now")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx slo reports](gcx_slo_reports.md)	 - Manage SLO reports.

