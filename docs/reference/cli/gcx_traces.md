## gcx traces

Query Tempo datasources and manage Adaptive Traces

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for traces
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

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx traces adaptive](gcx_traces_adaptive.md)	 - Manage Adaptive Traces resources
* [gcx traces get](gcx_traces_get.md)	 - Retrieve a trace by ID
* [gcx traces labels](gcx_traces_labels.md)	 - List trace labels or label values
* [gcx traces metrics](gcx_traces_metrics.md)	 - Execute a TraceQL metrics query
* [gcx traces query](gcx_traces_query.md)	 - Search for traces using a TraceQL query

