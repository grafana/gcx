## gcx datasources mysql

Query MySQL datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for mysql
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
* [gcx datasources mysql describe-table](gcx_datasources_mysql_describe-table.md)	 - Show the columns of a MySQL table
* [gcx datasources mysql list-tables](gcx_datasources_mysql_list-tables.md)	 - List tables from a MySQL datasource
* [gcx datasources mysql query](gcx_datasources_mysql_query.md)	 - Execute a SQL query against a MySQL datasource

