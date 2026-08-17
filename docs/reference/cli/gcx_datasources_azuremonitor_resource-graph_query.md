## gcx datasources azuremonitor resource-graph query

Query Azure Resource Graph with KQL

### Synopsis

Execute a KQL (Kusto Query Language) query against Azure Resource Graph,
Azure's inventory of resources across subscriptions.

KQL is the query expression, e.g. 'Resources | project name, type | limit 10'.
Pass --subscription (repeatable) to scope the query.

Datasource is resolved from -d flag or datasources.azuremonitor in your context.

Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources azuremonitor resource-graph query KQL [flags]
```

### Examples

```

  # List resources by type
  gcx datasources azuremonitor resource-graph query \
    'Resources | summarize count() by type | order by count_ desc' \
    -d UID --subscription SUB_ID

  # Query across multiple subscriptions, output as JSON
  gcx datasources azuremonitor resource-graph query 'Resources | project name, type, location' \
    -d UID --subscription SUB_A --subscription SUB_B -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources azuremonitor resource-graph query 'Resources | project name, type' \
    -d UID --subscription SUB_ID --share-link

  # Open the executed query in Grafana Explore
  gcx datasources azuremonitor resource-graph query 'Resources | project name, type' \
    -d UID --subscription SUB_ID --open
```

### Options

```
  -d, --datasource string          Datasource UID (required unless datasources.azuremonitor is configured)
  -h, --help                       help for query
      --jq string                  jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                       Open the executed query in Grafana Explore
  -o, --output string              Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link                 Print the Grafana Explore URL for the executed query to stderr
      --subscription stringArray   Azure subscription ID to query (repeatable; at least one required)
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

* [gcx datasources azuremonitor resource-graph](gcx_datasources_azuremonitor_resource-graph.md)	 - Query Azure Resource Graph with KQL

