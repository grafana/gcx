## gcx alert notification-history alerts

List the alerts in a single notification.

### Synopsis

List the individual alerts that were part of one grouped notification.

The notification's own entry does not carry its alerts, so they are fetched
separately by UUID. The time range must bracket the notification's timestamp;
widen --since (or set --from/--to) if the notification is older.

```
gcx alert notification-history alerts [flags]
```

### Options

```
      --from string      Start of time range (RFC3339). Overrides --since.
  -h, --help             help for alerts
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of alerts to return. (default 100)
  -o, --output string    Output format. One of: agents, json, table, yaml (default "table")
      --since duration   Look back this far from now when --from is not set. (default 1h0m0s)
      --to string        End of time range (RFC3339, default now).
      --uuid string      UUID of the notification (from 'notification-history list').
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

* [gcx alert notification-history](gcx_alert_notification-history.md)	 - Inspect alert notification delivery history.

