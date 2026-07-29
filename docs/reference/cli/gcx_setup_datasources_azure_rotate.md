## gcx setup datasources azure rotate

Rotate client secrets for gcx-created Azure datasources

### Synopsis

Mint a fresh client secret for each gcx-managed Azure datasource, update the datasource to use it, and retire the superseded secret. Only datasources whose backing app registration is tagged gcx-managed (and attributable to you) are rotated; key-based datasources (Cosmos DB) are skipped.

```
gcx setup datasources azure rotate [flags]
```

### Examples

```

  # Preview which datasources would be rotated
  gcx setup datasources azure rotate --dry-run

  # Rotate all gcx-managed Azure datasource secrets
  gcx setup datasources azure rotate --force
```

### Options

```
      --config string            Path to the configuration file to use
      --context string           Name of the context to use
      --dry-run                  Preview which datasources would have their secret rotated without changing anything
      --force                    Confirm credential-mutating side effects (required in agent mode)
  -h, --help                     help for rotate
      --jq string                jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string            Output format. One of: agents, json, text, yaml (default "text")
      --secret-expiry-days int   Set an expiry (in days) on the newly minted client secrets (0 = Azure default)
      --skip-health-check        Skip the post-rotation datasource health verification
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

* [gcx setup datasources azure](gcx_setup_datasources_azure.md)	 - Onboard Azure datasources (Azure Monitor, Azure Data Explorer, and Azure CosmosDB)

