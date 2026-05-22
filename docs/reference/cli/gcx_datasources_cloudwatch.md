## gcx datasources cloudwatch

Query AWS CloudWatch datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for cloudwatch
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx datasources cloudwatch list-accounts](gcx_datasources_cloudwatch_list-accounts.md)	 - List AWS accounts accessible via cross-account monitoring
* [gcx datasources cloudwatch list-dimensions](gcx_datasources_cloudwatch_list-dimensions.md)	 - List available dimension keys for a CloudWatch metric
* [gcx datasources cloudwatch list-metrics](gcx_datasources_cloudwatch_list-metrics.md)	 - List available CloudWatch metrics in a namespace
* [gcx datasources cloudwatch list-namespaces](gcx_datasources_cloudwatch_list-namespaces.md)	 - List available CloudWatch namespaces
* [gcx datasources cloudwatch list-regions](gcx_datasources_cloudwatch_list-regions.md)	 - List available AWS regions for the CloudWatch datasource
* [gcx datasources cloudwatch query](gcx_datasources_cloudwatch_query.md)	 - Execute a CloudWatch metric query

