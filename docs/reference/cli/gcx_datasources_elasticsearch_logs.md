## gcx datasources elasticsearch logs

Query logs from an Elasticsearch datasource

### Synopsis

Query log documents from an Elasticsearch datasource with a Lucene query,
newest first. Plugin-internal fields (_source, sort, highlight) are omitted.

EXPR is a Lucene query string; omit it to match all documents in the time range.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.

```
gcx datasources elasticsearch logs [EXPR] [flags]
```

### Examples

```

  # Latest logs from the last hour
  gcx datasources elasticsearch logs --since 1h

  # Filter by field
  gcx datasources elasticsearch logs -d UID 'level:error' --since 6h --limit 50

  # Output as JSON
  gcx datasources elasticsearch logs -d UID 'app:frontend' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.elasticsearch is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for logs
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max log lines to return (capped at 1000) (default 100)
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
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

