## gcx frontend apps list-sessions

List sessions that have replay recordings.

### Synopsis

Queries Loki for faro.session_recording.started events to discover sessions with replay data.

```
gcx frontend apps list-sessions <app-name> [flags]
```

### Examples

```
  # List sessions with replays in the last hour.
  gcx frontend apps list-sessions my-web-app-42

  # Search the last 24 hours.
  gcx frontend apps list-sessions my-web-app-42 --since 24h

  # Use a specific Loki datasource.
  gcx frontend apps list-sessions my-web-app-42 -d P8E80F9AEF21F6940
```

### Options

```
  -d, --datasource string   Loki datasource UID (auto-discovered if omitted)
  -h, --help                help for list-sessions
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum log lines to scan (default 1000)
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
      --since string        How far back to search (e.g., 1h, 24h, 7d) (default "1h")
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

