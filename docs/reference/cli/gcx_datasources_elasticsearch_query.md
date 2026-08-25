## gcx datasources elasticsearch query

Search documents in an Elasticsearch datasource

### Synopsis

Search documents in an Elasticsearch datasource with a Lucene query.

EXPR is a Lucene query string (e.g. 'app:frontend AND level:error'); omit it to
match all documents in the time range. The index pattern comes from the
datasource configuration.

--mode documents (default) returns raw source documents. --mode logs returns
the same documents newest-first with plugin-internal fields (_source, sort,
highlight, _type) omitted, matching how Grafana Explore's Logs view reads them.

Datasource is resolved from -d flag or datasources.elasticsearch in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources elasticsearch query [EXPR] [flags]
```

### Examples

```

  # Match all documents in the last hour
  gcx datasources elasticsearch query --since 1h

  # Lucene query with explicit datasource
  gcx datasources elasticsearch query -d UID 'app:frontend AND level:error' --since 1h

  # Newest-first logs, plugin-internal fields omitted
  gcx datasources elasticsearch query -d UID 'level:error' --mode logs --since 6h --limit 50

  # Output as JSON, limit results
  gcx datasources elasticsearch query -d UID 'datacenter:us-east' --limit 20 -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources elasticsearch query 'level:error' --since 1h --share-link

  # Continue the same search in Grafana Explore
  gcx datasources elasticsearch query 'level:error' --since 1h --open
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.elasticsearch is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int           Max documents to return (1-1000) (default 100)
      --mode string         Search mode: "documents" (raw documents) or "logs" (newest-first, plugin-internal fields omitted) (default "documents")
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

