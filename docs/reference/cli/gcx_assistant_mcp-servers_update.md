## gcx assistant mcp-servers update

Update an Assistant MCP server.

### Synopsis

Update an Assistant MCP server integration.

Partial updates are merged with the current server before saving. Scope, name,
and url are the server's immutable identity: they cannot be changed via update —
delete and recreate the server to change them. Headers follow an explicit
write-intent model: a --header with a value overwrites (or creates) that header;
if you pass no --header flags at all, every existing header is preserved
unchanged; but once you pass any --header flags, they become the full desired
header list, so any existing header you don't list is removed.

```
gcx assistant mcp-servers update <id-or-name> [flags]
```

### Examples

```
  gcx assistant mcp-servers update GitHub --disabled
  gcx assistant mcp-servers update SharedTools --description "Shared internal MCP tools"
  gcx assistant mcp-servers update SharedTools --header "X-API-Key=<token>"
```

### Options

```
      --application stringArray   Assistant application allowed to use this server (repeatable)
      --description string        MCP server description
      --disabled                  Disable the MCP server
      --enabled                   Enable the MCP server
  -f, --file string               Read MCP server input from a YAML or JSON file
      --header stringArray        Custom header as NAME=VALUE (repeatable; tenant scope requires an auth header)
  -h, --help                      help for update
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string               MCP server display name
  -o, --output string             Output format. One of: agents, json, yaml (default "yaml")
      --scope string              MCP server scope: user or tenant
      --url string                Remote MCP server URL
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant mcp-servers](gcx_assistant_mcp-servers.md)	 - Manage Assistant MCP server integrations.

