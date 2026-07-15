## gcx assistant conversation get

Get a conversation transcript

### Synopsis

Fetch conversation metadata and message history for a conversation ID.

Use this to pull a web Assistant chat into a coding agent before continuing it
with 'gcx assistant prompt --context-id'.

```
gcx assistant conversation get <conversation-id> [flags]
```

### Examples

```
  gcx assistant conversation get 295a674f-3a3d-44e8-9166-3f8054409f65
  gcx assistant conversation get 295a674f-3a3d-44e8-9166-3f8054409f65 -o json
```

### Options

```
  -h, --help            help for get
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --timeout int     HTTP timeout in seconds (default 60)
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

* [gcx assistant conversation](gcx_assistant_conversation.md)	 - Read Grafana Assistant conversations

