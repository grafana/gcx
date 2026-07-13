## gcx datasources azuremonitor list-metrics

List available Azure Monitor metrics for a resource

### Synopsis

List the Azure Monitor metric definitions available for a resource, including
each metric's primary aggregation, unit, and dimensions.

```
gcx datasources azuremonitor list-metrics [flags]
```

### Examples

```

  gcx datasources azuremonitor list-metrics -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts --resource mystorage

  gcx datasources azuremonitor list-metrics -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines --resource my-vm -o json
```

### Options

```
  -d, --datasource string       Datasource UID (required unless datasources.azuremonitor is configured)
  -h, --help                    help for list-metrics
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string        Metric namespace, e.g. Microsoft.Storage/storageAccounts (required; matches the resource type from list-resources)
  -o, --output string           Output format. One of: agents, json, table, yaml (default "table")
      --resource string         Azure resource name (required)
      --resource-group string   Azure resource group name (required)
      --subscription string     Azure subscription ID (required)
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

