## gcx datasources postgres

Query PostgreSQL datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for postgres
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
* [gcx datasources postgres describe-table](gcx_datasources_postgres_describe-table.md)	 - Show the columns of a PostgreSQL table
* [gcx datasources postgres list-tables](gcx_datasources_postgres_list-tables.md)	 - List tables from a PostgreSQL datasource
* [gcx datasources postgres query](gcx_datasources_postgres_query.md)	 - Execute a SQL query against a PostgreSQL datasource

