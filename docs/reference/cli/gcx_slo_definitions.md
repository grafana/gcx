## gcx slo definitions

Manage SLO definitions.

### Synopsis

Manage SLO definitions.

Freeform queries must use $__rate_interval in every rate() and increase() range.
Literal ranges such as [5m] are rejected by the SLO API.

### Options

```
  -h, --help   help for definitions
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

* [gcx slo](gcx_slo.md)	 - Manage Grafana SLO definitions and reports
* [gcx slo definitions delete](gcx_slo_definitions_delete.md)	 - Delete SLO definitions.
* [gcx slo definitions get](gcx_slo_definitions_get.md)	 - Get a single SLO definition.
* [gcx slo definitions list](gcx_slo_definitions_list.md)	 - List SLO definitions.
* [gcx slo definitions pull](gcx_slo_definitions_pull.md)	 - Pull SLO definitions to disk (Deprecated: use gcx resources pull).
* [gcx slo definitions push](gcx_slo_definitions_push.md)	 - Push SLO from files (Deprecated: use gcx resources push).
* [gcx slo definitions status](gcx_slo_definitions_status.md)	 - Show SLO definitions status with SLI and error budget data.
* [gcx slo definitions timeline](gcx_slo_definitions_timeline.md)	 - Render SLI values over time as a line chart.

