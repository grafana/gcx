## grafanactl slo definitions status

Show SLO definitions status with SLI and error budget data.

### Synopsis

Show SLO definitions status by combining the SLO API with Prometheus metrics.

Displays current SLI, error budget, and health status for each SLO definition.
Requires that the SLO destination datasource has recording rules generating
grafana_slo_* metrics.

```
grafanactl slo definitions status [UUID] [flags]
```

### Examples

```
  # Show status of all SLO definitions.
  grafanactl slo definitions status

  # Show status of a specific SLO by UUID.
  grafanactl slo definitions status abc123def

  # Show extended status with 1h/1d SLI columns.
  grafanactl slo definitions status -o wide

  # Output status as JSON for scripting.
  grafanactl slo definitions status -o json

  # Render a compliance summary bar chart.
  grafanactl slo definitions status -o graph
```

### Options

```
  -h, --help            help for status
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: graph, json, table, wide, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl slo definitions](grafanactl_slo_definitions.md)	 - Manage SLO definitions.

