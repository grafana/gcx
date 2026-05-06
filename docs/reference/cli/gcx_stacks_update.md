## gcx stacks update

Update a Grafana Cloud stack.

### Synopsis

Update a Grafana Cloud stack.

This command modifies a live Grafana Cloud stack. Changing the name or disabling
delete protection can have downstream effects. Always confirm the intended
changes with the user and prefer --dry-run first.

```
gcx stacks update <stack-slug> [flags]
```

### Options

```
      --delete-protection      Enable delete protection
      --description string     New description
      --dry-run                Preview the request without executing it
  -h, --help                   help for update
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --labels strings         Labels in key=value format (replaces all labels)
      --name string            New stack name
      --no-delete-protection   Disable delete protection
  -o, --output string          Output format. One of: json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx stacks](gcx_stacks.md)	 - Manage Grafana Cloud stacks (list, create, update, delete)

