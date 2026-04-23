## gcx datasources infinity query

Fetch data from a URL or inline source via the Infinity datasource

### Synopsis

Fetch JSON, CSV, TSV, XML, GraphQL, or HTML data through a Grafana Infinity datasource.

URL is the target endpoint passed as a positional argument.
Use --inline to provide data directly instead of fetching from a URL.
Datasource is resolved from -d flag or datasources.infinity in your context.

```
gcx datasources infinity query [URL] [flags]
```

### Examples

```

  # Fetch JSON from a URL
  gcx datasources infinity query https://api.example.com/users --type json

  # Fetch with a JSONPath root selector
  gcx datasources infinity query https://api.example.com/data --type json --root '$.items'

  # Inline JSON data
  gcx datasources infinity query --inline '[{"name":"alice"},{"name":"bob"}]' --type json

  # GraphQL query
  gcx datasources infinity query https://api.example.com/graphql --type graphql --graphql 'query { users { id name } }'

  # CSV with custom headers
  gcx datasources infinity query https://example.com/data.csv --type csv --header 'Authorization=Bearer token'

  # Output as JSON
  gcx datasources infinity query -d UID https://api.example.com/data -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.infinity is configured)
      --from string          Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --graphql string       GraphQL query string
      --header stringArray   Custom header in key=value format (repeatable)
  -h, --help                 help for query
      --inline string        Inline data string (replaces URL)
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --method string        HTTP method: GET or POST (default "GET")
  -o, --output string        Output format. One of: json, table, wide, yaml (default "table")
      --root string          Root selector (JSONPath for JSON, XPath for XML/HTML)
      --since string         Duration before --to (or now if omitted); mutually exclusive with --from
      --to string            End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string          Data type: json, csv, tsv, xml, graphql, html (default "json")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources infinity](gcx_datasources_infinity.md)	 - Query Infinity datasources (JSON, CSV, XML, GraphQL from any URL)

