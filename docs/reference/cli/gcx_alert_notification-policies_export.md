## gcx alert notification-policies export

Export the notification policy tree in provisioning format.

```
gcx alert notification-policies export [flags]
```

### Options

```
      --format string   Export format: yaml, json, or hcl (default "yaml")
  -h, --help            help for export
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

* [gcx alert notification-policies](gcx_alert_notification-policies.md)	 - Manage the Grafana alerting notification policy tree.

