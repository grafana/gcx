## gcx alert state-history

Inspect alert state history.

### Synopsis

Query the recorded history of alert rule state transitions.

State history is served by Grafana's alerting history backend. The default Loki
backend answers both rule-scoped and global queries; the annotations backend
requires --rule. Records are returned newest-first.

  gcx alert state-history list --rule <uid> --from now-24h
  gcx alert state-history list --label severity=critical --limit 200

### Options

```
  -h, --help   help for state-history
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

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx alert state-history list](gcx_alert_state-history_list.md)	 - List recorded alert state transitions.

