## gcx datasources tempo

Query Tempo datasources

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for tempo
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

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx datasources tempo get](gcx_datasources_tempo_get.md)	 - Retrieve a trace by ID
* [gcx datasources tempo labels](gcx_datasources_tempo_labels.md)	 - List trace labels or label values
* [gcx datasources tempo metrics](gcx_datasources_tempo_metrics.md)	 - Execute a TraceQL metrics query
* [gcx datasources tempo query](gcx_datasources_tempo_query.md)	 - Search for traces using a TraceQL query

