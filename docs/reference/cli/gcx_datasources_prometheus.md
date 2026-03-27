## gcx datasources prometheus

Prometheus datasource operations

### Synopsis

Operations specific to Prometheus datasources such as labels, metadata, and targets.

### Options

```
  -h, --help   help for prometheus
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage Grafana datasources
* [gcx datasources prometheus labels](gcx_datasources_prometheus_labels.md)	 - List labels or label values
* [gcx datasources prometheus metadata](gcx_datasources_prometheus_metadata.md)	 - Get metric metadata
* [gcx datasources prometheus query](gcx_datasources_prometheus_query.md)	 - Execute a PromQL query against a Prometheus datasource
* [gcx datasources prometheus targets](gcx_datasources_prometheus_targets.md)	 - List scrape targets

