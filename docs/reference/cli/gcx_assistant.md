## gcx assistant

Interact with Grafana Assistant

### Synopsis

Send prompts to Grafana Assistant and receive streaming responses via the A2A protocol.

### Options

```
  -h, --help   help for assistant
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string   Name of the context to use (overrides current-context in config)
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx assistant prompt](gcx_assistant_prompt.md)	 - Send a single message to Grafana Assistant

