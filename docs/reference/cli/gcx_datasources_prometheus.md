## gcx datasources prometheus

Query Prometheus datasources

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for prometheus
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx datasources prometheus labels](gcx_datasources_prometheus_labels.md)	 - List labels or label values
* [gcx datasources prometheus metadata](gcx_datasources_prometheus_metadata.md)	 - Get metric metadata
* [gcx datasources prometheus query](gcx_datasources_prometheus_query.md)	 - Execute a PromQL query against a Prometheus datasource

