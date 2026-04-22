## gcx logs adaptive segments create

Create an adaptive log segment.

```
gcx logs adaptive segments create [flags]
```

### Options

```
      --fallback-to-default   Fall back to default segment
  -h, --help                  help for create
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string           Segment name (required)
  -o, --output string         Output format. One of: json, yaml (default "json")
      --selector string       Log stream selector (required)
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

* [gcx logs adaptive segments](gcx_logs_adaptive_segments.md)	 - Manage adaptive log segments.

