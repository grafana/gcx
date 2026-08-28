## gcx alert notification-history

Inspect alert notification delivery history.

### Synopsis

Inspect the history of alert notifications delivered by Grafana Alerting.

These commands are read-only. Each entry is a grouped notification that Grafana
attempted to send to a contact point, recorded by the alerting historian. Use
'list' to browse notifications and 'alerts' to see the alerts in a specific one.

Notification history must be enabled on the stack (the
[unified_alerting.notification_history] config with Loki, plus the
kubernetesAlertingHistorian feature).

### Options

```
  -h, --help   help for notification-history
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
* [gcx alert notification-history alerts](gcx_alert_notification-history_alerts.md)	 - List the alerts in a single notification.
* [gcx alert notification-history list](gcx_alert_notification-history_list.md)	 - List notification delivery history.

