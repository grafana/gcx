## grafanactl alert rules list

List alert rules.

```
grafanactl alert rules list [flags]
```

### Options

```
      --folder string   Filter by folder UID
      --group string    Filter by group name
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
      --state string    Filter by rule state (firing, pending, inactive)
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

* [grafanactl alert rules](grafanactl_alert_rules.md)	 - Manage alert rules.

