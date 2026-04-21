## gcx datasources influxdb

Query InfluxDB datasources

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for influxdb
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
* [gcx datasources influxdb field-keys](gcx_datasources_influxdb_field-keys.md)	 - List field keys
* [gcx datasources influxdb measurements](gcx_datasources_influxdb_measurements.md)	 - List measurements
* [gcx datasources influxdb query](gcx_datasources_influxdb_query.md)	 - Execute a query against an InfluxDB datasource

