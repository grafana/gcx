## gcx datasources opensearch list-fields

List mapped fields from an OpenSearch datasource

### Synopsis

List the mapped fields and their types, per index. Nested object fields are
flattened with dotted names. Use these names in Lucene queries and --group-by.

```
gcx datasources opensearch list-fields [flags]
```

### Examples

```

  # All fields across indices
  gcx datasources opensearch list-fields

  # Fields of one index
  gcx datasources opensearch list-fields -d UID --index grafana-logs -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.opensearch is configured)
  -h, --help                help for list-fields
      --index string        Restrict to this index or index pattern
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx datasources opensearch](gcx_datasources_opensearch.md)	 - Query OpenSearch datasources

