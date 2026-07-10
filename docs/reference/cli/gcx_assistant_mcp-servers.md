## gcx assistant mcp-servers

Manage Assistant MCP server integrations.

### Synopsis

Manage remote MCP server integrations in the current Grafana stack's Assistant settings.

MCP servers can be scoped to the current user ("user", shown as "Just me" in
Grafana) or to the stack tenant ("tenant", shown as "Everybody" in Grafana).
Tenant-scoped servers are shared and must be configured with a non-empty
authentication header such as Authorization, X-API-Key, or X-Grafana-API-Key.

OAuth-based MCP servers, such as GitHub Copilot, are user-scoped. When Grafana
reports that OAuth is required after create or update, gcx initiates the
Assistant OAuth flow and opens the authorization URL in a browser.

### Examples

```
  # List configured MCP servers as text table output
  gcx assistant mcp-servers list

  # Add a user-scoped OAuth MCP server and open the authorization URL
  gcx assistant mcp-servers create --name GitHub --url https://api.githubcopilot.com/mcp

  # Add a tenant-scoped header-auth MCP server
  gcx assistant mcp-servers create --name SharedTools --url https://mcp.example.com/mcp \
    --scope tenant --header "Authorization=Bearer <token>"
```

### Options

```
  -h, --help   help for mcp-servers
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

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant
* [gcx assistant mcp-servers create](gcx_assistant_mcp-servers_create.md)	 - Create an Assistant MCP server.
* [gcx assistant mcp-servers delete](gcx_assistant_mcp-servers_delete.md)	 - Delete an Assistant MCP server.
* [gcx assistant mcp-servers get](gcx_assistant_mcp-servers_get.md)	 - Get an Assistant MCP server.
* [gcx assistant mcp-servers list](gcx_assistant_mcp-servers_list.md)	 - List Assistant MCP servers.
* [gcx assistant mcp-servers update](gcx_assistant_mcp-servers_update.md)	 - Update an Assistant MCP server.

