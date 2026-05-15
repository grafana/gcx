## gcx assistant prompt

Send a single message to Grafana Assistant

### Synopsis

Send a single message to Grafana Assistant and receive the response.

This is useful for scripting and automation. The response streams via
the A2A (Agent-to-Agent) protocol over Server-Sent Events.

Known agent IDs:
  grafana_assistant_cli   General-purpose assistant (default)
  grafana_dashboarding    Dashboard builder — queries live Prometheus to discover
                          metrics and returns complete dashboard JSON ready for
                          'gcx resources push'. See also: gcx assistant dashboard

```
gcx assistant prompt <message> [flags]
```

### Examples

```
  gcx assistant prompt "What alerts are firing?"
  gcx assistant prompt "Show CPU usage" --json
  gcx assistant prompt "Follow up" --continue
  gcx assistant prompt "Build a CPU dashboard" --agent-id grafana_dashboarding
```

### Options

```
      --agent-id string     Agent ID to target (e.g. grafana_assistant_cli, grafana_dashboarding) (default "grafana_assistant_cli")
      --context-id string   Context ID for conversation threading
      --continue            Continue the previous chat session
  -h, --help                help for prompt
      --json                Output as JSON (streams NDJSON events by default)
      --no-stream           With --json, emit a single JSON object instead of streaming events
      --timeout int         Timeout in seconds when waiting for a response (default 300)
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

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant

