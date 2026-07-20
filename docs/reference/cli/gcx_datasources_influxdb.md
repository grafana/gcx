## gcx datasources influxdb

Query InfluxDB datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for influxdb
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
* [gcx datasources influxdb list-field-keys](gcx_datasources_influxdb_list-field-keys.md)	 - List field keys
* [gcx datasources influxdb list-measurements](gcx_datasources_influxdb_list-measurements.md)	 - List measurements
* [gcx datasources influxdb list-tag-keys](gcx_datasources_influxdb_list-tag-keys.md)	 - List tag keys
* [gcx datasources influxdb list-tag-values](gcx_datasources_influxdb_list-tag-values.md)	 - List tag values
* [gcx datasources influxdb query](gcx_datasources_influxdb_query.md)	 - Execute a query against an InfluxDB datasource

