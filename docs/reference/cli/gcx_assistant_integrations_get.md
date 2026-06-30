## gcx assistant integrations get

Get integration detail.

### Synopsis

Get the full detail of a specific integration by ID.

```
gcx assistant integrations get <id> [flags]
```

### Examples

```
  gcx assistant integrations get 550e8400-e29b-41d4-a716-446655440000
  gcx assistant integrations get 550e8400-e29b-41d4-a716-446655440000 -o json
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx assistant integrations](gcx_assistant_integrations.md)	 - Manage Grafana Assistant integrations.

