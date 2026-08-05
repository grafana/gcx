## gcx datasources azuremonitor list-subscriptions

List Azure subscriptions visible to the datasource

### Synopsis

List the Azure subscriptions the Azure Monitor datasource's credentials can access.

```
gcx datasources azuremonitor list-subscriptions [flags]
```

### Examples

```

  gcx datasources azuremonitor list-subscriptions -d UID
  gcx datasources azuremonitor list-subscriptions -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.azuremonitor is configured)
  -h, --help                help for list-subscriptions
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
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

