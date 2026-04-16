## gcx alert templates

Manage Grafana alerting notification templates.

### Options

```
  -h, --help   help for templates
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
* [gcx alert templates delete](gcx_alert_templates_delete.md)	 - Delete a notification template by name.
* [gcx alert templates get](gcx_alert_templates_get.md)	 - Get a notification template by name.
* [gcx alert templates list](gcx_alert_templates_list.md)	 - List notification templates.
* [gcx alert templates upsert](gcx_alert_templates_upsert.md)	 - Create or update a notification template.

