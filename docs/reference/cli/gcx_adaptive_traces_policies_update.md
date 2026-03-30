## gcx adaptive traces policies update

Update an Adaptive Traces sampling policy by ID.

```
gcx adaptive traces policies update <id> [flags]
```

### Options

```
  -f, --filename string   File containing the policy definition (use - for stdin)
  -h, --help              help for update
      --json string       Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string     Output format. One of: json, yaml (default "yaml")
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

* [gcx adaptive traces policies](gcx_adaptive_traces_policies.md)	 - Manage Adaptive Traces sampling policies.

