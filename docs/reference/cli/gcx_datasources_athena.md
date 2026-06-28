## gcx datasources athena

Query Amazon Athena datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for athena
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
* [gcx datasources athena describe-table](gcx_datasources_athena_describe-table.md)	 - Show column schema for an Athena table
* [gcx datasources athena list-catalogs](gcx_datasources_athena_list-catalogs.md)	 - List available Athena data catalogs
* [gcx datasources athena list-databases](gcx_datasources_athena_list-databases.md)	 - List databases in an Athena data catalog
* [gcx datasources athena list-tables](gcx_datasources_athena_list-tables.md)	 - List tables in an Athena database
* [gcx datasources athena query](gcx_datasources_athena_query.md)	 - Execute a SQL query against an Athena datasource

