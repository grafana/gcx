## gcx slo reports status

Show SLO report status with combined SLI and error budget data.

### Synopsis

Show SLO report status by aggregating health data across all SLOs in each report.

Fetches report definitions, resolves referenced SLO UUIDs, queries Prometheus
metrics, and computes combined SLI and error budget per report.

```
gcx slo reports status [UUID] [flags]
```

### Examples

```
  # Show status of all SLO reports.
  gcx slo reports status

  # Show status of a specific report by UUID.
  gcx slo reports status abc123def

  # Show extended status with per-SLO breakdown.
  gcx slo reports status -o wide

  # Output status as JSON for scripting.
  gcx slo reports status -o json

  # Render a combined SLI bar chart.
  gcx slo reports status -o graph
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
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx slo reports](gcx_slo_reports.md)	 - Manage SLO reports.

