## gcx mcp serve

Start an MCP server on stdio

### Synopsis

Start an MCP server listening on stdin/stdout using the JSON-RPC protocol. This is intended for use with AI agent tools like Claude Code.

```
gcx mcp serve [flags]
```

### Options

```
  -h, --help   help for serve
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx mcp](gcx_mcp.md)	 - Model Context Protocol server

