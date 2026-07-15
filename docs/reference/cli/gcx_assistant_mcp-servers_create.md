## gcx assistant mcp-servers create

Create an Assistant MCP server.

### Synopsis

Create an Assistant MCP server integration.

By default, servers are user-scoped. Use --scope tenant for a shared server.
Tenant-scoped servers require at least one non-empty authentication header, such
as Authorization, X-API-Key, or X-Grafana-API-Key. OAuth-based servers should be
created with user scope; gcx opens the OAuth authorization URL when Grafana
reports that OAuth is required.

```
gcx assistant mcp-servers create [flags]
```

### Examples

```
  gcx assistant mcp-servers create --name GitHub --url https://api.githubcopilot.com/mcp

  gcx assistant mcp-servers create --name SharedTools --url https://mcp.example.com/mcp \
    --scope tenant --header "Authorization=Bearer <token>"

  gcx assistant mcp-servers create --file server.yaml --if-not-exists
```

### Options

```
      --application stringArray   Assistant application allowed to use this server (repeatable)
      --description string        MCP server description
      --disabled                  Disable the MCP server
      --enabled                   Enable the MCP server
  -f, --file string               Read MCP server input from a YAML or JSON file
      --header stringArray        Custom header as NAME=VALUE (repeatable; tenant scope requires an auth header)
  -h, --help                      help for create
      --if-not-exists             Return an existing server with the same name, URL, and scope instead of failing
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

