## gcx datasources azuremonitor query

Execute an Azure Monitor metrics query

### Synopsis

Execute an Azure Monitor metrics query.

Queries are structured (subscription, resource group, resource, metric namespace,
metric, aggregation) — there is no expression language for Azure Monitor metrics.
Use --dimensions (repeatable) to filter or split by dimension values: a specific
value filters the series, "*" splits the result into one series per value.

Use the list-subscriptions, list-resource-groups, list-resources, and
list-metrics subcommands to discover valid flag values.

Datasource is resolved from -d flag or datasources.azuremonitor in your context.
Note: datasources configured with "Current User" (Azure AD passthrough)
authentication cannot be queried with API tokens or service accounts.

```
gcx datasources azuremonitor query [flags]
```

### Examples

```

  # Query a storage account metric
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts \
    --resource mystorage --metric Transactions --aggregation Total --since 1h

  # Split the series by a dimension
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts \
    --resource mystorage --metric Transactions --aggregation Total \
    --dimensions ApiName='*' --since 1h

  # Output as JSON
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines \
    --resource my-vm --metric 'Percentage CPU' -o json
```

### Options

```
      --aggregation string          Aggregation: Average, Total, Maximum, Minimum, or Count (must be supported by the metric; see list-metrics) (default "Average")
  -d, --datasource string           Datasource UID (required unless datasources.azuremonitor is configured)
      --dimensions stringToString   Dimension key=value filters (repeatable, e.g. --dimensions ApiName=GetBlob); use "*" as the value to split the result by that dimension (default [])
      --from string                 Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                        help for query
      --jq string                   jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                 Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --metric string               Metric name, e.g. Transactions (required)
      --namespace string            Metric namespace, e.g. Microsoft.Storage/storageAccounts (required)
  -o, --output string               Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --region string               Azure region, e.g. uksouth (optional; used for multi-resource queries)
      --resource string             Azure resource name (required)
      --resource-group string       Azure resource group name (required)
      --since string                Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --subscription string         Azure subscription ID (required)
      --time-grain string           Time grain as an ISO 8601 duration (e.g. PT1M, PT1H) or "auto" to fit the time range (default "auto")
      --to string                   End time (RFC3339, Unix timestamp, or relative like 'now')
      --top string                  Maximum number of dimension value series to return (only with --dimensions)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources azuremonitor](gcx_datasources_azuremonitor.md)	 - Query Azure Monitor datasources

