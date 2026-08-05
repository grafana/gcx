## gcx datasources

Manage and query Grafana datasources

### Synopsis

List, inspect, and query Grafana datasources. Use top-level signal commands (metrics, logs, traces, profiles) for datasource-specific queries.

### Options

```
  -h, --help   help for datasources
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

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx datasources athena](gcx_datasources_athena.md)	 - Query Amazon Athena datasources
* [gcx datasources azuremonitor](gcx_datasources_azuremonitor.md)	 - Query Azure Monitor datasources
* [gcx datasources clickhouse](gcx_datasources_clickhouse.md)	 - Query ClickHouse datasources
* [gcx datasources cloudwatch](gcx_datasources_cloudwatch.md)	 - Query AWS CloudWatch datasources
* [gcx datasources create](gcx_datasources_create.md)	 - Create a datasource from a manifest file
* [gcx datasources delete](gcx_datasources_delete.md)	 - Delete one or more datasources
* [gcx datasources get](gcx_datasources_get.md)	 - Get details of a specific datasource
* [gcx datasources health](gcx_datasources_health.md)	 - Check the health of one or more datasources
* [gcx datasources infinity](gcx_datasources_infinity.md)	 - Query Infinity datasources (JSON, CSV, XML, GraphQL from any URL)
* [gcx datasources influxdb](gcx_datasources_influxdb.md)	 - Query InfluxDB datasources
* [gcx datasources list](gcx_datasources_list.md)	 - List all datasources
* [gcx datasources loki](gcx_datasources_loki.md)	 - Query Loki datasources
* [gcx datasources prometheus](gcx_datasources_prometheus.md)	 - Query Prometheus datasources
* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources
* [gcx datasources query](gcx_datasources_query.md)	 - Execute a query against any datasource (auto-detects type)
* [gcx datasources schemas](gcx_datasources_schemas.md)	 - Inspect datasource plugin schemas
* [gcx datasources tempo](gcx_datasources_tempo.md)	 - Query Tempo datasources
* [gcx datasources update](gcx_datasources_update.md)	 - Update a datasource from a manifest file

