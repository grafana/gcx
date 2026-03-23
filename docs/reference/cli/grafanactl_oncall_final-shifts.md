## grafanactl oncall final-shifts

List final shifts for a schedule.

```
grafanactl oncall final-shifts <schedule-id> [flags]
```

### Options

```
      --end string      End date (YYYY-MM-DD) (default "2026-03-30")
  -h, --help            help for final-shifts
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, table, yaml (default "table")
      --start string    Start date (YYYY-MM-DD) (default "2026-03-23")
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

* [grafanactl oncall](grafanactl_oncall.md)	 - Manage Grafana OnCall resources.

