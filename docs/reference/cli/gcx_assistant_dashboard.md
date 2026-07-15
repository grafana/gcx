## gcx assistant dashboard

Build a dashboard using the Grafana dashboarding agent

### Synopsis

Send a dashboard creation request to the Grafana dashboarding agent.

The agent queries live Prometheus to discover available clusters and metric
names, then returns complete dashboard JSON that can be pushed directly with
'gcx resources push'.

This is equivalent to:
  gcx assistant prompt --agent-id grafana_dashboarding <message>

Note: each request consumes billable Grafana Assistant tokens, including
requests made through gcx. See https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing.md.

```
gcx assistant dashboard <message> [flags]
```

### Examples

```
  gcx assistant dashboard "Build a CPU usage dashboard across all clusters"
  gcx assistant dashboard "Create a dashboard for HTTP error rates by service" --json
```

### Options

```
      --context-id string   Context ID for conversation threading
      --continue            Continue the previous chat session
  -h, --help                help for dashboard
      --json                Output as JSON (streams NDJSON events by default)
      --no-stream           With --json, emit a single JSON object instead of streaming events
      --timeout int         Timeout in seconds when waiting for a response (default 300)
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

