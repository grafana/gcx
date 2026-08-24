## gcx assistant prompt

Send a single message to Grafana Assistant

### Synopsis

Send a single message to Grafana Assistant and receive the response.

This is useful for scripting and automation. The response streams via
the A2A (Agent-to-Agent) protocol over Server-Sent Events.

Use --agent-id to target a specific A2A agent (default: grafana_assistant_cli).

Note: each prompt consumes billable Grafana Assistant tokens, including requests
made through gcx. See https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing.md.

```
gcx assistant prompt <message> [flags]
```

### Examples

```
  gcx assistant prompt "What alerts are firing?"
  gcx assistant prompt "Show CPU usage" --json
  gcx assistant prompt "Follow up" --continue
```

### Options

```
      --agent-id string     Agent ID to target (default: grafana_assistant_cli) (default "grafana_assistant_cli")
      --context-id string   Context ID for conversation threading
      --continue            Continue the previous chat session
  -h, --help                help for prompt
      --json                Output as JSON (streams NDJSON events by default)
      --no-stream           With --json, emit a single JSON object instead of streaming events
      --timeout int         Timeout in seconds when waiting for a response (default 300)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant

