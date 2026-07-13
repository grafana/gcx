## gcx datasources azuremonitor list-resources

List Azure resources in a subscription or resource group

### Synopsis

List the Azure resources in a subscription, optionally scoped to a resource group, via an Azure Monitor datasource.

```
gcx datasources azuremonitor list-resources [flags]
```

### Examples

```

  gcx datasources azuremonitor list-resources -d UID --subscription SUB_ID --resource-group my-rg
  gcx datasources azuremonitor list-resources -d UID --subscription SUB_ID -o json
```

### Options

```
  -d, --datasource string       Datasource UID (required unless datasources.azuremonitor is configured)
  -h, --help                    help for list-resources
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string           Output format. One of: agents, json, table, yaml (default "table")
      --resource-group string   Azure resource group name (optional; lists the whole subscription when omitted)
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

