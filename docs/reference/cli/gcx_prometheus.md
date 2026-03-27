## gcx prometheus

Prometheus datasource operations

### Synopsis

Operations specific to Prometheus datasources such as labels, metadata, and targets.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for prometheus
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - 
* [gcx prometheus labels](gcx_prometheus_labels.md)	 - List labels or label values
* [gcx prometheus metadata](gcx_prometheus_metadata.md)	 - Get metric metadata
* [gcx prometheus query](gcx_prometheus_query.md)	 - Execute a PromQL query against a Prometheus datasource
* [gcx prometheus targets](gcx_prometheus_targets.md)	 - List scrape targets

