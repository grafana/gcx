## gcx datasources athena list-databases

List databases in an Athena data catalog

```
gcx datasources athena list-databases [flags]
```

### Examples

```

  # List databases in the default catalog
  gcx datasources athena list-databases -d UID --catalog AwsDataCatalog

  # With region override
  gcx datasources athena list-databases -d UID --catalog AwsDataCatalog --region us-east-1
```

### Options

```
      --catalog string      Data catalog to query
  -d, --datasource string   Datasource UID (required unless datasources.athena is configured)
  -h, --help                help for list-databases
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --region string       AWS region override
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

* [gcx datasources athena](gcx_datasources_athena.md)	 - Query Amazon Athena datasources

