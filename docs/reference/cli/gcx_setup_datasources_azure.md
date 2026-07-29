## gcx setup datasources azure

Onboard Azure datasources (Azure Monitor, Azure Data Explorer, and Azure CosmosDB)

### Synopsis

Discover Azure resources using your local `az` CLI session and provision the matching Grafana datasources. For each accepted datasource, gcx mints a dedicated, gcx-owned Azure app registration (service principal + secret + least-privilege role) with you set as owner, then creates the Grafana datasource wired to those credentials.

```
gcx setup datasources azure [flags]
```

### Examples

```

  # Interactive: discover and pick datasources to create
  gcx setup datasources azure

  # Preview what would be created without minting anything
  gcx setup datasources azure --dry-run

  # Non-interactive: create the Azure Monitor datasource with a specified subscription
  gcx setup datasources azure --force --types azure-monitor --subscription <sub-id>

  # Tighten Azure Monitor permissions to metrics only
  gcx setup datasources azure --types azure-monitor --role "Monitoring Reader"

  # Remove everything gcx created
  gcx setup datasources azure --cleanup --force
```

### Options

```
      --cleanup                  Remove gcx-created Azure app registrations and their datasources
      --config string            Path to the configuration file to use
      --context string           Name of the context to use
      --dry-run                  Preview what would be created or removed without making any changes
      --force                    Confirm credential-minting side effects (required in agent mode); implies --yes
  -h, --help                     help for azure
      --include-cosmos           Include Azure Cosmos DB datasources (requires the Enterprise plugin licensed in Grafana)
      --jq string                jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string            Output format. One of: agents, json, text, yaml (default "text")
      --role string              Override the default Azure role set (comma-separated role names, e.g. "Monitoring Reader")
      --secret-expiry-days int   Set an expiry (in days) on minted client secrets (0 = Azure default). Rotate before expiry with the rotate subcommand
      --skip-health-check        Skip the post-create datasource health verification
      --subscription strings     Subscription ID(s) to target (default: all discovered)
      --types strings            Restrict to datasource kinds: azure-monitor, adx, cosmos
      --yes                      Non-interactive: skip prompts and accept all suggestions
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx setup datasources](gcx_setup_datasources.md)	 - Set up cloud provider datasources
* [gcx setup datasources azure rotate](gcx_setup_datasources_azure_rotate.md)	 - Rotate client secrets for gcx-created Azure datasources

