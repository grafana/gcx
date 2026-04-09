## gcx logs adaptive drop-rules list

List adaptive log drop rules.

```
gcx logs adaptive drop-rules list [flags]
```

### Options

```
      --expiration-filter string   Filter by expiration: all, active, or expired (default "all")
  -h, --help                       help for list
      --json string                Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string              Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx logs adaptive drop-rules](gcx_logs_adaptive_drop-rules.md)	 - Manage adaptive log drop rules.

