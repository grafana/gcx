## gcx alert template-groups

Manage native notification template groups by title.

### Synopsis

Manage TemplateGroup manifests through Grafana's native notifications API.
List includes Grafana-managed groups and read-only imported Mimir groups.
Use templates for the existing provisioning-payload commands.

### Options

```
  -h, --help   help for template-groups
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
* [gcx alert template-groups create](gcx_alert_template-groups_create.md)	 - Create a Grafana-managed template group from a native manifest.
* [gcx alert template-groups delete](gcx_alert_template-groups_delete.md)	 - Delete one Grafana-managed template group by exact title.
* [gcx alert template-groups get](gcx_alert_template-groups_get.md)	 - Get a native template group manifest by exact title.
* [gcx alert template-groups list](gcx_alert_template-groups_list.md)	 - List all template groups, including read-only imports.
* [gcx alert template-groups update](gcx_alert_template-groups_update.md)	 - Update or rename the selected Grafana-managed template group.

