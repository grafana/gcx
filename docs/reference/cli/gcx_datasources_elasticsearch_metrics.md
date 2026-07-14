## gcx datasources elasticsearch metrics

Aggregate documents over time from an Elasticsearch datasource

### Synopsis

Run a metric aggregation bucketed by a time histogram, optionally split
into series by a terms field.

EXPR is a Lucene query string scoping the documents; omit it to aggregate all.
Returns (time, value, series) rows. Use --step to control bucket size.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.

```
gcx datasources elasticsearch metrics [EXPR] [flags]
```

### Examples

```

  # Document count over time
  gcx datasources elasticsearch metrics --since 6h

  # Error count per app
  gcx datasources elasticsearch metrics 'level:error' --group-by app.keyword --since 6h

  # Average value of a numeric field
  gcx datasources elasticsearch metrics --agg avg --field duration_ms --since 1h -o json
```

### Options

```
      --agg string          Metric aggregation: count, avg, sum, min, max, or cardinality (default "count")
  -d, --datasource string   Datasource UID (required unless datasources.elasticsearch is configured)
      --expr string         Query expression (alternative to positional argument)
      --field string        Field to aggregate (required unless --agg count)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --group-by string     Split series by this field's terms (use .keyword for text fields)
      --group-size int      Max number of series when using --group-by (default 10)
  -h, --help                help for metrics
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --time-field string   Time field for the date histogram (default "@timestamp")
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

