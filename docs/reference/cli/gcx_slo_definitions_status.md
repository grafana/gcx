## gcx slo definitions status

Show SLO definitions status with SLI and error budget data.

### Synopsis

Show SLO definitions status by combining the SLO API with Prometheus metrics.

Displays current SLI, error budget, and health status for each SLO definition.
Requires that the SLO destination datasource has recording rules generating
grafana_slo_* metrics.

```
gcx slo definitions status [UUID] [flags]
```

### Examples

```
  # Show status of all SLO definitions.
  gcx slo definitions status

  # Show status of a specific SLO by UUID.
  gcx slo definitions status abc123def

  # Show extended status with 1h/1d SLI columns.
  gcx slo definitions status -o wide

  # Output status as JSON for scripting.
  gcx slo definitions status -o json

  # Render a compliance summary bar chart.
  gcx slo definitions status -o graph
```

### Options

```
  -h, --help            help for status
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: graph, json, table, wide, yaml (default "table")
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

