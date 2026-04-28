## gcx alert

Manage Grafana alert rules and alert groups

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for alert
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx alert contact-points](gcx_alert_contact-points.md)	 - Manage Grafana alerting contact points.
* [gcx alert groups](gcx_alert_groups.md)	 - Manage alert rule groups.
* [gcx alert instances](gcx_alert_instances.md)	 - Manage alert instances.
* [gcx alert mute-timings](gcx_alert_mute-timings.md)	 - Manage Grafana alerting mute timings.
* [gcx alert notification-policies](gcx_alert_notification-policies.md)	 - Manage the Grafana alerting notification policy tree.
* [gcx alert rules](gcx_alert_rules.md)	 - Manage alert rules.
* [gcx alert templates](gcx_alert_templates.md)	 - Manage Grafana alerting notification templates.

