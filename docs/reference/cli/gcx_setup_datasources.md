## gcx setup datasources

Set up cloud provider datasources

### Synopsis

Discover cloud resources using your local cloud CLI session and provision the matching Grafana datasources, minting dedicated, gcx-owned credentials per datasource.

Azure is supported today (Azure Monitor, Azure Data Explorer, and Azure CosmosDB). AWS and GCP are planned.

### Options

```
  -h, --help   help for datasources
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

* [gcx setup](gcx_setup.md)	 - Onboard and configure Grafana Cloud products.
* [gcx setup datasources azure](gcx_setup_datasources_azure.md)	 - Onboard Azure datasources (Azure Monitor, Azure Data Explorer, and Azure CosmosDB)

