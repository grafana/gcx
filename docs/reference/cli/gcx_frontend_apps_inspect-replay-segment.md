## gcx frontend apps inspect-replay-segment

Inspect events for a session replay segment.

```
gcx frontend apps inspect-replay-segment <app-name> <session-id> <segment-id> [flags]
```

### Examples

```
  # Show event summary for replay segment 0.
  gcx frontend apps inspect-replay-segment my-web-app-42 abc-session-123 0

  # Save full replay event JSON to a file.
  gcx frontend apps inspect-replay-segment my-web-app-42 abc-session-123 0 --save events.json

  # Output full replay event JSON.
  gcx frontend apps inspect-replay-segment my-web-app-42 abc-session-123 0 -o json
```

### Options

```
  -h, --help                  help for inspect-replay-segment
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string         Output format. One of: agents, json, text, yaml (default "text")
      --recording-id string   Replay recording ID to use (defaults to the first recording for the session ID)
      --save string           Write full replay event JSON to a file
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

