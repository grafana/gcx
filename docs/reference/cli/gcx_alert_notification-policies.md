## gcx alert notification-policies

Manage the Grafana alerting notification policy tree.

### Options

```
  -h, --help   help for notification-policies
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

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx alert notification-policies export](gcx_alert_notification-policies_export.md)	 - Export the notification policy tree in provisioning format.
* [gcx alert notification-policies get](gcx_alert_notification-policies_get.md)	 - Get the notification policy tree.
* [gcx alert notification-policies reset](gcx_alert_notification-policies_reset.md)	 - Reset the notification policy tree to its default.
* [gcx alert notification-policies set](gcx_alert_notification-policies_set.md)	 - Replace the entire notification policy tree.

