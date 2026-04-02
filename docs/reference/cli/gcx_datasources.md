## gcx datasources

Manage and query Grafana datasources

### Synopsis

List, inspect, and query Grafana datasources. Use top-level signal commands (metrics, logs, traces, profiles) for datasource-specific queries.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for datasources
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
* [gcx datasources get](gcx_datasources_get.md)	 - Get details of a specific datasource
* [gcx datasources list](gcx_datasources_list.md)	 - List all datasources
* [gcx datasources query](gcx_datasources_query.md)	 - Execute a query against any datasource (auto-detects type)

