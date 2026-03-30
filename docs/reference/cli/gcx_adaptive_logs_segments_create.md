## gcx adaptive logs segments create

Create an adaptive log segment.

```
gcx adaptive logs segments create [flags]
```

### Options

```
      --fallback-to-default   Fall back to default segment
  -h, --help                  help for create
      --json string           Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --name string           Segment name (required)
  -o, --output string         Output format. One of: json, yaml (default "json")
      --selector string       Log stream selector
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

* [gcx adaptive logs segments](gcx_adaptive_logs_segments.md)	 - Manage adaptive log segments.

