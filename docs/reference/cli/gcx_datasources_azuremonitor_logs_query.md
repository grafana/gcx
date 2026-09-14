## gcx datasources azuremonitor logs query

Query a Log Analytics workspace with KQL

### Synopsis

Execute a KQL (Kusto Query Language) query against an Azure Log Analytics
workspace.

KQL is the query expression, e.g. 'AppRequests | take 10'. The workspace is
identified by --subscription, --resource-group, and --workspace; use
list-resources to discover workspaces (type Microsoft.OperationalInsights/workspaces).

Datasource is resolved from -d flag or datasources.azuremonitor in your context.

Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources azuremonitor logs query KQL [flags]
```

### Examples

```

  # Query a workspace
  gcx datasources azuremonitor logs query 'AppRequests | take 10' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace

  # With a time range
  gcx datasources azuremonitor logs query 'AppRequests | summarize count() by bin(TimeGenerated, 5m)' \
    -d UID --subscription SUB_ID --resource-group my-rg --workspace my-workspace --since 1h

  # Output as JSON
  gcx datasources azuremonitor logs query 'AppTraces | take 5' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources azuremonitor logs query 'AppRequests | take 10' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace --share-link

  # Open the executed query in Grafana Explore
  gcx datasources azuremonitor logs query 'AppRequests | take 10' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace --open
```

### Options

```
  -d, --datasource string       Datasource UID (required unless datasources.azuremonitor is configured)
      --from string             Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                    help for query
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --open                    Open the executed query in Grafana Explore
  -o, --output string           Output format. One of: agents, json, table, wide, yaml (default "table")
      --resource-group string   Azure resource group of the workspace (required)
      --share-link              Print the Grafana Explore URL for the executed query to stderr
      --since string            Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --subscription string     Azure subscription ID (required)
      --to string               End time (RFC3339, Unix timestamp, or relative like 'now')
      --workspace string        Log Analytics workspace name (required)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources azuremonitor logs](gcx_datasources_azuremonitor_logs.md)	 - Query Log Analytics workspaces with KQL

