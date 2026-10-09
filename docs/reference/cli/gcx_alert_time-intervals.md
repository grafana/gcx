## gcx alert time-intervals

Manage notification time intervals by title.

### Synopsis

Manage time intervals through Grafana's native notifications API. Native
JSON/YAML manifests also work with generic resources commands. Titles are matched
exactly and case-sensitively across the complete inventory; API names are opaque.
Use export for provisioning YAML/JSON/HCL, rather than a native manifest.

### Options

```
  -h, --help   help for time-intervals
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
* [gcx alert time-intervals create](gcx_alert_time-intervals_create.md)	 - Create a new time interval from a native manifest.
* [gcx alert time-intervals delete](gcx_alert_time-intervals_delete.md)	 - Delete a time interval by exact title after confirmation.
* [gcx alert time-intervals export](gcx_alert_time-intervals_export.md)	 - Export time intervals as provisioning YAML, JSON, or HCL.
* [gcx alert time-intervals get](gcx_alert_time-intervals_get.md)	 - Get a native time interval manifest by exact title.
* [gcx alert time-intervals list](gcx_alert_time-intervals_list.md)	 - List all time intervals with their titles and API names.
* [gcx alert time-intervals update](gcx_alert_time-intervals_update.md)	 - Replace an existing time interval using its fetched version.

