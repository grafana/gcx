## gcx datasources infinity query

Query a pre-configured Infinity datasource

### Synopsis

Query a Grafana Infinity datasource using its saved configuration.

The datasource's URL, type, method, and headers are read from its saved configuration.
EXPR is an optional root selector expression (JSONPath for JSON, XPath for XML/HTML)
that narrows the returned data.

Datasource is resolved from -d flag or datasources.infinity in your context.

```
gcx datasources infinity query [EXPR] [flags]
```

### Examples

```

  # Query using datasource UID
  gcx datasources infinity query -d UID

  # Narrow results with a JSONPath expression
  gcx datasources infinity query -d UID '$.items'

  # Output as JSON
  gcx datasources infinity query -d UID '$.results' -o json

  # Query with a time range
  gcx datasources infinity query -d UID --from now-24h --to now
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.infinity is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
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

* [gcx datasources infinity](gcx_datasources_infinity.md)	 - Query Infinity datasources (JSON, CSV, XML, GraphQL from any URL)

