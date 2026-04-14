## gcx metrics adaptive exemptions list

List Adaptive Metrics recommendation exemptions.

```
gcx metrics adaptive exemptions list [flags]
```

### Options

```
      --all-segments     List exemptions across all segments
  -h, --help             help for list
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of exemptions to return (0 for no limit)
  -o, --output string    Output format. One of: json, table, wide, yaml (default "table")
      --segment string   Segment ID
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

* [gcx metrics adaptive exemptions](gcx_metrics_adaptive_exemptions.md)	 - Manage Adaptive Metrics recommendation exemptions.

