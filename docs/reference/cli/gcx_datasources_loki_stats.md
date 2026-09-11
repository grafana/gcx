## gcx datasources loki stats

Show index stats (streams/chunks/bytes/entries) for a LogQL selector without executing it

### Synopsis

Query Loki's index-stats endpoint for a label matcher and time range.

Returns stream/chunk/byte/entry counts WITHOUT executing the query — useful to
estimate the cost of a query before running it with 'loki query' or 'loki metrics'.

EXPR is the LogQL expression to evaluate (the same expression accepted by
'query'/'metrics' can be reused as-is) — only its stream selector(s) (the
'{...}' matcher) are sent to the index-stats endpoint, since Loki's index only
tracks streams, not line filters or parsing stages, aggregations, or range
vectors. The estimate reflects all data in the matched streams, not the
narrower set a filter like '|= "error"' would actually return. An expression
combining multiple selectors (e.g. via a binary operator) sums each
selector's stats into a single total.
When no time flags are given, defaults to the last minute (now-1m to now),
matching the instant-query default used by 'query'/'metrics'. That window is
widened by any range-vector duration or offset in EXPR (e.g. '[24h]',
'offset 1h'), since Loki evaluates further back than --from/--to/--since
alone would suggest.

```
gcx datasources loki stats [EXPR] [flags]
```

### Examples

```

  # Estimate bytes scanned by a selector over the last hour
  gcx datasources loki stats -d UID '{job="varlogs"}' --since 1h

  # Output as JSON
  gcx datasources loki stats -d UID '{job="varlogs"}' --since 1h -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --expr string         LogQL expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for stats
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
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

* [gcx datasources loki](gcx_datasources_loki.md)	 - Query Loki datasources

