## gcx assistant

Interact with Grafana Assistant

### Synopsis

Send prompts to Grafana Assistant and receive streaming responses via the A2A protocol.

Requires Grafana Cloud with OAuth authentication (gcx login with browser flow).
Service account tokens are not supported.

Note: Grafana Assistant is billed based on tokens consumed, including requests
made through gcx. See https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing.md.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for assistant
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx assistant conversation](gcx_assistant_conversation.md)	 - Read Grafana Assistant conversations
* [gcx assistant dashboard](gcx_assistant_dashboard.md)	 - Build a dashboard using the Grafana dashboarding agent
* [gcx assistant investigations](gcx_assistant_investigations.md)	 - Manage Grafana Assistant investigations.
* [gcx assistant mcp-servers](gcx_assistant_mcp-servers.md)	 - Manage Assistant MCP server integrations.
* [gcx assistant prompt](gcx_assistant_prompt.md)	 - Send a single message to Grafana Assistant

