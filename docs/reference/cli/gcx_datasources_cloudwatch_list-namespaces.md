## gcx datasources cloudwatch list-namespaces

List available CloudWatch namespaces

### Synopsis

List the CloudWatch namespaces visible in the given region from a CloudWatch datasource.

```
gcx datasources cloudwatch list-namespaces [flags]
```

### Examples

```

  gcx datasources cloudwatch list-namespaces -d UID --region us-east-1
  gcx datasources cloudwatch list-namespaces -d UID --region us-east-1 -o json
```

### Options

```
      --account-id string   AWS account ID for cross-account monitoring (or 'all')
  -d, --datasource string   Datasource UID (required unless datasources.cloudwatch is configured)
  -h, --help                help for list-namespaces
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --region string       AWS region (required)
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

* [gcx datasources cloudwatch](gcx_datasources_cloudwatch.md)	 - Query AWS CloudWatch datasources

