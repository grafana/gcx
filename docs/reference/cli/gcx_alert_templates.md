## gcx alert templates

Manage Grafana alerting notification templates.

### Options

```
  -h, --help   help for templates
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
* [gcx alert templates delete](gcx_alert_templates_delete.md)	 - Delete a notification template by name.
* [gcx alert templates get](gcx_alert_templates_get.md)	 - Get a notification template by name.
* [gcx alert templates list](gcx_alert_templates_list.md)	 - List notification templates.
* [gcx alert templates upsert](gcx_alert_templates_upsert.md)	 - Create or update a notification template.

