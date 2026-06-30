## gcx assistant integrations update

Update an integration.

### Synopsis

Update an existing integration. Fetches the current state and applies changes.

```
gcx assistant integrations update <id> [flags]
```

### Examples

```
  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --name="renamed"
  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --enabled=false
  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --url="https://new-mcp.example.com"
```

### Options

```
      --applications strings    Target applications (assistant, loop, all)
      --configuration string    Integration configuration as JSON
      --custom-header strings   Custom HTTP headers (key=value, repeatable)
      --description string      New description
      --enabled                 Whether the integration is enabled (default true)
  -h, --help                    help for update
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string             New integration name
  -o, --output string           Output format. One of: agents, json, yaml (default "yaml")
      --url string              MCP server URL (shortcut for --configuration)
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

