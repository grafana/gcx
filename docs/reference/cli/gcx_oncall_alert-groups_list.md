## gcx oncall alert-groups list

List alert groups.

```
gcx oncall alert-groups list [flags]
```

### Options

```
  -h, --help             help for list
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-age string   Exclude groups older than this duration (e.g. 1h, 24h, 7d)
  -o, --output string    Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx oncall alert-groups](gcx_oncall_alert-groups.md)	 - Manage alert groups.

