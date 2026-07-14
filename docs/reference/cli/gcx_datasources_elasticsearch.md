## gcx datasources elasticsearch

Query Elasticsearch datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for elasticsearch
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
* [gcx datasources elasticsearch fields](gcx_datasources_elasticsearch_fields.md)	 - List mapped fields from an Elasticsearch datasource
* [gcx datasources elasticsearch list-indices](gcx_datasources_elasticsearch_list-indices.md)	 - List indices from an Elasticsearch datasource
* [gcx datasources elasticsearch logs](gcx_datasources_elasticsearch_logs.md)	 - Query logs from an Elasticsearch datasource
* [gcx datasources elasticsearch metrics](gcx_datasources_elasticsearch_metrics.md)	 - Aggregate documents over time from an Elasticsearch datasource
* [gcx datasources elasticsearch query](gcx_datasources_elasticsearch_query.md)	 - Search documents in an Elasticsearch datasource

