## gcx frontend apps list-replay-sessions

List Frontend Observability sessions that have replay recordings.

### Synopsis

Discovers regular session IDs that have replay recordings by querying Loki or Pinot for faro.session_recording.started events. This does not list all Frontend Observability sessions. The default datasource is Loki; pass a Pinot datasource UID with -d to query Pinot. An empty result means no replay-start event was found for the app ID and time window; this command does not verify that the app exists. JSON output has an items envelope and includes list_meta when the event scan reaches its limit.

```
gcx frontend apps list-replay-sessions <slug-id-or-numeric-id> [flags]
```

### Examples

```
  # List regular session IDs with replay recordings in the last hour.
  gcx frontend apps list-replay-sessions my-web-app-42

  # Search the last 24 hours.
  gcx frontend apps list-replay-sessions my-web-app-42 --since 24h

  # Use a specific Loki or Pinot datasource.
  gcx frontend apps list-replay-sessions my-web-app-42 -d P8E80F9AEF21F6940
```

### Options

```
  -d, --datasource string   Loki or Pinot datasource UID (Loki auto-discovered if omitted)
  -h, --help                help for list-replay-sessions
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum replay-start events to scan (gcx caps Loki scans at 1000; not the number of sessions) (default 1000)
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
      --since string        How far back to search (e.g., 1h, 24h, 7d) (default "1h")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

