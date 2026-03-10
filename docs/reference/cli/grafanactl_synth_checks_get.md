## grafanactl synth checks get

Get a single Synthetic Monitoring check.

```
grafanactl synth checks get ID [flags]
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
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

* [grafanactl synth checks](grafanactl_synth_checks.md)	 - Manage Synthetic Monitoring checks.

