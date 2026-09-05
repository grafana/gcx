## gcx datasources elasticsearch list-indices

List indices from an Elasticsearch datasource

### Synopsis

List the indices visible to an Elasticsearch datasource, with their mapped
field counts. Pass --index to restrict to one index or pattern; fetching the
mapping for every index can hit the response size cap on a large cluster.

```
gcx datasources elasticsearch list-indices [flags]
```

### Examples

```

  gcx datasources elasticsearch list-indices
  gcx datasources elasticsearch list-indices -d UID -o json

  # Restrict to one index or pattern
  gcx datasources elasticsearch list-indices -d UID --index grafana-logs
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.elasticsearch is configured)
  -h, --help                help for list-indices
      --index string        Restrict to this index or index pattern
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
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

* [gcx datasources elasticsearch](gcx_datasources_elasticsearch.md)	 - Query Elasticsearch datasources

