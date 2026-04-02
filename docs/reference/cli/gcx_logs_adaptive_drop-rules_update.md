## gcx logs adaptive drop-rules update

Update an adaptive log drop rule.

```
gcx logs adaptive drop-rules update ID [flags]
```

### Options

```
      --body string         JSON object for the rule body
      --body-file string    Path to a JSON file for the rule body
      --disabled            Whether the rule is disabled
      --expires-at string   RFC3339 expiry timestamp (omit flag to leave unchanged)
  -h, --help                help for update
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --name string         Rule name
  -o, --output string       Output format. One of: json, yaml (default "json")
      --version int         Policy body schema version
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

