## gcx alert routing-trees

Manage notification routing trees (default and named).

### Synopsis

Manage notification routing trees through Grafana's native
notifications.alerting.grafana.app API.

The default tree is named "user-defined". Named trees require Grafana 13.1+,
or 12.4-13.0 with the alertingMultiplePolicies feature toggle. Server errors
are reported unchanged.

### Options

```
  -h, --help   help for routing-trees
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
* [gcx alert routing-trees create](gcx_alert_routing-trees_create.md)	 - Create a named routing tree from a manifest.
* [gcx alert routing-trees delete](gcx_alert_routing-trees_delete.md)	 - Delete a named routing tree, or reset the default tree.
* [gcx alert routing-trees get](gcx_alert_routing-trees_get.md)	 - Get a routing tree manifest by name.
* [gcx alert routing-trees list](gcx_alert_routing-trees_list.md)	 - List routing trees, including the default tree.
* [gcx alert routing-trees update](gcx_alert_routing-trees_update.md)	 - Replace one routing tree from a manifest.

