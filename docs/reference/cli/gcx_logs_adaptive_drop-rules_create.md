## gcx logs adaptive drop-rules create

Create an adaptive log drop rule.

```
gcx logs adaptive drop-rules create [flags]
```

### Options

```
      --body string         JSON object for the rule body (e.g. v1 drop_rate, stream_selector, levels)
      --body-file string    Path to a JSON file for the rule body
      --disabled            Create the rule in a disabled state
      --expires-at string   Optional RFC3339 expiry timestamp
  -h, --help                help for create
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --name string         Rule name (required)
  -o, --output string       Output format. One of: json, yaml (default "json")
      --version int         Policy body schema version (default 1)
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

* [gcx logs adaptive drop-rules](gcx_logs_adaptive_drop-rules.md)	 - Manage adaptive log drop rules.

