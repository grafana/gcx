## grafanactl datasources prometheus

Prometheus datasource operations

### Synopsis

Operations specific to Prometheus datasources such as labels, metadata, and targets.

### Options

```
  -h, --help   help for prometheus
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl datasources](grafanactl_datasources.md)	 - Manage Grafana datasources
* [grafanactl datasources prometheus labels](grafanactl_datasources_prometheus_labels.md)	 - List labels or label values
* [grafanactl datasources prometheus metadata](grafanactl_datasources_prometheus_metadata.md)	 - Get metric metadata
* [grafanactl datasources prometheus targets](grafanactl_datasources_prometheus_targets.md)	 - List scrape targets

