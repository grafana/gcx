## gcx alert groups

Inspect alert rule groups and their evaluation status.

### Synopsis

Inspect Grafana-managed alert rule groups.

These commands are read-only. To modify the rules in a group, use the
resources commands: gcx resources pull/push alertrules.

### Options

```
  -h, --help   help for groups
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

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx alert groups get](gcx_alert_groups_get.md)	 - Get a single alert rule group.
* [gcx alert groups list](gcx_alert_groups_list.md)	 - List alert rule groups.
* [gcx alert groups status](gcx_alert_groups_status.md)	 - Show alert rule group status.

