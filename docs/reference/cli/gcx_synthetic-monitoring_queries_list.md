## gcx synthetic-monitoring queries list

List the named queries the Synthetic Monitoring datasource serves.

### Synopsis

List the named queries this tenant's Synthetic Monitoring datasource
publishes. Each entry can be run with 'gcx synthetic-monitoring query NAME'; use
'gcx synthetic-monitoring queries get NAME' for its full parameter schema.

Requires Synthetic Monitoring app v1.62.0 or later -- named-query discovery is
not available on older deployments.

```
gcx synthetic-monitoring queries list [flags]
```

### Examples

```
  gcx synthetic-monitoring queries list
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synthetic-monitoring queries](gcx_synthetic-monitoring_queries.md)	 - Discover Synthetic Monitoring named queries.

