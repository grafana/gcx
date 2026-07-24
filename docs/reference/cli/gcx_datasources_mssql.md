## gcx datasources mssql

Query Microsoft SQL Server datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for mssql
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
* [gcx datasources mssql describe-table](gcx_datasources_mssql_describe-table.md)	 - List columns for an MSSQL table
* [gcx datasources mssql list-tables](gcx_datasources_mssql_list-tables.md)	 - List tables and views in an MSSQL database
* [gcx datasources mssql query](gcx_datasources_mssql_query.md)	 - Execute a SQL query against an MSSQL datasource

