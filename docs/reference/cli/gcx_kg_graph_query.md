## gcx kg graph query

Run a read-only Cypher query and return its columns and rows.

### Synopsis

Run a read-only Cypher query against the Knowledge Graph using /v1/query/cypher.

Run 'gcx kg meta schema' to discover entity types, properties, and relationships.
The time window defaults to the last hour. Use --since or both --from and --to.
Every matched node and relationship must overlap the requested window; this is
not a historical snapshot and does not require an entire path to coexist.

The response contains columns, rows, and stats (rowCount, columnCount, elapsedMs).
Scalars, nulls, lists, maps, nodes, relationships, and paths retain their JSON
shape. Table output uses the projected columns; nested values render as JSON.
Node/relationship refs are local to one response, not durable graph identities.
Use --json columns,rows,stats to select envelope fields or --jq '.rows' for rows.

A literal LIMIT is required in the query. Results describe that bounded query,
not a complete graph inventory; there is no lastPage indicator or automatic paging.
Use literal SKIP/LIMIT in Cypher to select a result window, with ORDER BY for
predictable ordering. The legacy entities/edges envelope, --page, and
--insights-only remain available via the deprecated 'gcx kg entities query'.
Use 'gcx kg entities list' with --insight for entity health evidence.
Caller parameters are not supported. Use literals and escape strings as Cypher literals.
Variable-length paths require a finite upper bound within the server's hop limit.
The server validates its language allowlist and execution budgets; refusals include
stable CYPHER_* error codes. There is no fallback to the legacy search endpoint.

```
gcx kg graph query <cypher-query> [flags]
```

### Examples

```
  gcx kg graph query "MATCH (s:Service) RETURN s.name AS name LIMIT 10"
  gcx kg graph query "MATCH (s:Service) RETURN count(s) AS services LIMIT 1" --since 1h
  gcx kg graph query "MATCH p = (s:Service {name: 'checkout'})-[:CALLS*1..3]->(d) RETURN p, length(p) AS hops LIMIT 5" --since 24h
```

### Options

```
      --from string     Start time (RFC3339, Unix seconds, or relative like 'now-1h'); requires --to
  -h, --help            help for query
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
      --since string    Positive duration ending now (e.g. 1h, 30m, 7d); defaults to 1h; mutually exclusive with --from/--to
      --to string       End time (RFC3339, Unix seconds, or relative like 'now'); requires --from
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg graph](gcx_kg_graph.md)	 - Query Knowledge Graph projections and paths.

