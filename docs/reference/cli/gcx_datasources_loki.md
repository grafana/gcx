## gcx datasources loki

Loki datasource operations

### Synopsis

Operations specific to Loki datasources such as labels and series.

### Options

```
  -h, --help   help for loki
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
* [gcx datasources loki labels](gcx_datasources_loki_labels.md)	 - List labels or label values
* [gcx datasources loki query](gcx_datasources_loki_query.md)	 - Execute a LogQL query against a Loki datasource
* [gcx datasources loki series](gcx_datasources_loki_series.md)	 - List log streams

