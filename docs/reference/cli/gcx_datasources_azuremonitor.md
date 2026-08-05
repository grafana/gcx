## gcx datasources azuremonitor

Query Azure Monitor datasources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for azuremonitor
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
* [gcx datasources azuremonitor list-metrics](gcx_datasources_azuremonitor_list-metrics.md)	 - List available Azure Monitor metrics for a resource
* [gcx datasources azuremonitor list-resource-groups](gcx_datasources_azuremonitor_list-resource-groups.md)	 - List resource groups in an Azure subscription
* [gcx datasources azuremonitor list-resources](gcx_datasources_azuremonitor_list-resources.md)	 - List Azure resources in a subscription or resource group
* [gcx datasources azuremonitor list-subscriptions](gcx_datasources_azuremonitor_list-subscriptions.md)	 - List Azure subscriptions visible to the datasource
* [gcx datasources azuremonitor logs](gcx_datasources_azuremonitor_logs.md)	 - Query a Log Analytics workspace with KQL
* [gcx datasources azuremonitor query](gcx_datasources_azuremonitor_query.md)	 - Execute an Azure Monitor metrics query
* [gcx datasources azuremonitor resource-graph](gcx_datasources_azuremonitor_resource-graph.md)	 - Query Azure Resource Graph with KQL

