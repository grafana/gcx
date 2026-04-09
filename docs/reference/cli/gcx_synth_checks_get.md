## gcx synth checks get

Get a single Synthetic Monitoring check.

```
gcx synth checks get NAME [flags]
```

### Examples

```
  # Get check by resource name (from 'gcx synth checks list').
  gcx synth checks get grafana-instance-health-5594

  # Get check by numeric ID.
  gcx synth checks get 5594

  # Get check with current execution status.
  gcx synth checks get grafana-instance-health-5594 --show-status
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
      --show-status     Query and display the check's current execution status from Prometheus
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

* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.

