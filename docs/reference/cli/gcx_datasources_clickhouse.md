## gcx datasources clickhouse

Query ClickHouse datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for clickhouse
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx datasources clickhouse describe-table](gcx_datasources_clickhouse_describe-table.md)	 - Show column schema for a ClickHouse table
* [gcx datasources clickhouse list-tables](gcx_datasources_clickhouse_list-tables.md)	 - List tables from a ClickHouse datasource
* [gcx datasources clickhouse query](gcx_datasources_clickhouse_query.md)	 - Execute a SQL query against a ClickHouse datasource

