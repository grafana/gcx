## gcx metrics

Query Prometheus datasources and manage Adaptive Metrics

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for metrics
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx metrics adaptive](gcx_metrics_adaptive.md)	 - Manage Adaptive Metrics resources
* [gcx metrics labels](gcx_metrics_labels.md)	 - List labels or label values
* [gcx metrics metadata](gcx_metrics_metadata.md)	 - Get metric metadata
* [gcx metrics query](gcx_metrics_query.md)	 - Execute a PromQL query against a Prometheus datasource
* [gcx metrics targets](gcx_metrics_targets.md)	 - List scrape targets

