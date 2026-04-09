## gcx logs

Query Loki datasources and manage Adaptive Logs

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for logs
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx logs adaptive](gcx_logs_adaptive.md)	 - Manage Adaptive Logs resources
* [gcx logs labels](gcx_logs_labels.md)	 - List labels or label values
* [gcx logs metrics](gcx_logs_metrics.md)	 - Execute a metric LogQL query against a Loki datasource
* [gcx logs query](gcx_logs_query.md)	 - Execute a LogQL query against a Loki datasource
* [gcx logs series](gcx_logs_series.md)	 - List log streams

