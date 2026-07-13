## gcx datasources azuremonitor logs

Query a Log Analytics workspace with KQL

### Synopsis

Execute a KQL (Kusto Query Language) query against an Azure Log Analytics
workspace.

KQL is the query expression, e.g. 'AppRequests | take 10'. The workspace is
identified by --subscription, --resource-group, and --workspace; use
list-resources to discover workspaces (type Microsoft.OperationalInsights/workspaces).

Datasource is resolved from -d flag or datasources.azuremonitor in your context.

```
gcx datasources azuremonitor logs KQL [flags]
```

### Examples

```

  # Query a workspace
  gcx datasources azuremonitor logs 'AppRequests | take 10' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace

  # With a time range
  gcx datasources azuremonitor logs 'AppRequests | summarize count() by bin(TimeGenerated, 5m)' \
    -d UID --subscription SUB_ID --resource-group my-rg --workspace my-workspace --since 1h

  # Output as JSON
  gcx datasources azuremonitor logs 'AppTraces | take 5' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace -o json
```

### Options

```
  -d, --datasource string       Datasource UID (required unless datasources.azuremonitor is configured)
      --from string             Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                    help for logs
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string           Output format. One of: agents, json, table, wide, yaml (default "table")
      --resource-group string   Azure resource group of the workspace (required)
      --since string            Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --subscription string     Azure subscription ID (required)
      --to string               End time (RFC3339, Unix timestamp, or relative like 'now')
      --workspace string        Log Analytics workspace name (required)
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

