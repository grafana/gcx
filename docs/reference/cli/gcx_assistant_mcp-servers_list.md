## gcx assistant mcp-servers list

List Assistant MCP servers.

### Synopsis

List Assistant MCP server integrations.

The default output format is a table. Use --output wide to include scope and
applications, or --output json, yaml, or agents for machine-readable output.

```
gcx assistant mcp-servers list [flags]
```

### Examples

```
  gcx assistant mcp-servers list
  gcx assistant mcp-servers list --output wide
  gcx assistant mcp-servers list --output json
```

### Options

```
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of integrations to request (default 50)
      --offset int      Number of integrations to skip
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx assistant mcp-servers](gcx_assistant_mcp-servers.md)	 - Manage Assistant MCP server integrations.

