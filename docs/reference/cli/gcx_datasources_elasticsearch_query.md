## gcx datasources elasticsearch query

Search documents in an Elasticsearch datasource

### Synopsis

Search documents in an Elasticsearch datasource with a Lucene query.

EXPR is a Lucene query string (e.g. 'app:frontend AND level:error'); omit it to
match all documents in the time range. The index pattern comes from the
datasource configuration.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.

```
gcx datasources elasticsearch query [EXPR] [flags]
```

### Examples

```

  # Match all documents in the last hour
  gcx datasources elasticsearch query --since 1h

  # Lucene query with explicit datasource
  gcx datasources elasticsearch query -d UID 'app:frontend AND level:error' --since 1h

  # Output as JSON, limit results
  gcx datasources elasticsearch query -d UID 'datacenter:us-east' --size 20 -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.elasticsearch is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --size int            Max documents to return (capped at 1000) (default 100)
      --step string         Query step (e.g., '15s', '1m')
      --time-field string   Time field used for range filtering (default "@timestamp")
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources elasticsearch](gcx_datasources_elasticsearch.md)	 - Query Elasticsearch datasources

