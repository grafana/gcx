## gcx assistant mcp-servers delete

Delete an Assistant MCP server.

### Synopsis

Delete an Assistant MCP server integration.

The command prompts for confirmation by default. Use --force to bypass the
prompt. GCX_AUTO_APPROVE also bypasses the prompt for non-interactive workflows,
while agent mode still requires explicit --force for destructive operations.

```
gcx assistant mcp-servers delete <id-or-name> [flags]
```

### Options

```
      --force           Delete without confirmation
  -h, --help            help for delete
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, yaml (default "yaml")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant mcp-servers](gcx_assistant_mcp-servers.md)	 - Manage Assistant MCP server integrations.

