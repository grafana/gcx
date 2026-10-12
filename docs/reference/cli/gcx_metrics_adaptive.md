## gcx metrics adaptive

Manage Adaptive Metrics resources

### Options

```
  -h, --help   help for adaptive
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

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics
* [gcx metrics adaptive exemptions](gcx_metrics_adaptive_exemptions.md)	 - Manage Adaptive Metrics recommendation exemptions.
* [gcx metrics adaptive recommendations](gcx_metrics_adaptive_recommendations.md)	 - Manage metric recommendations.
* [gcx metrics adaptive rules](gcx_metrics_adaptive_rules.md)	 - Manage aggregation rules.
* [gcx metrics adaptive segments](gcx_metrics_adaptive_segments.md)	 - Manage Adaptive Metrics segments.

