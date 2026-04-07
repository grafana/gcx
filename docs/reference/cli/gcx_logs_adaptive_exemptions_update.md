## gcx logs adaptive exemptions update

Update an adaptive log exemption.

```
gcx logs adaptive exemptions update ID [flags]
```

### Options

```
  -h, --help                     help for update
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string            Output format. One of: json, yaml (default "json")
      --reason string            Reason for the exemption
      --stream-selector string   Log stream selector
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

* [gcx logs adaptive exemptions](gcx_logs_adaptive_exemptions.md)	 - Manage adaptive log exemptions.

