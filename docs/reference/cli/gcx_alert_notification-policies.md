## gcx alert notification-policies

Manage the Grafana alerting notification policy tree.

### Options

```
  -h, --help   help for notification-policies
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx alert notification-policies export](gcx_alert_notification-policies_export.md)	 - Export the notification policy tree in provisioning format.
* [gcx alert notification-policies get](gcx_alert_notification-policies_get.md)	 - Get the notification policy tree.
* [gcx alert notification-policies reset](gcx_alert_notification-policies_reset.md)	 - Reset the notification policy tree to its default.
* [gcx alert notification-policies set](gcx_alert_notification-policies_set.md)	 - Replace the entire notification policy tree.

