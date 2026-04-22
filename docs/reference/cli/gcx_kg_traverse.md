## gcx kg traverse

Execute a Cypher query against the Knowledge Graph.

### Synopsis

Execute a Cypher query and return matching entities and relationships.

The Knowledge Graph uses FalkorDB Cypher syntax. Entity nodes are typed
(e.g. Service, Pod, Deployment) and edges represent relationships between them.

Use --with-insights to include active SAAFE insight counts on each entity.
Use --page to paginate large result sets (results are capped at 50 entities per page).

```
gcx kg traverse <cypher-query> [flags]
```

### Examples

```
  # Find services that call api-server
  gcx kg traverse "MATCH (s:Service)-[:CALLS]->(d:Service {name:'api-server'}) RETURN s, d"

  # List all pods for a service in a specific env
  gcx kg traverse "MATCH (s:Service {name:'checkout'})-[:hasInstance]->(p:Pod) RETURN s, p" --env prod

  # Include insight details on returned entities
  gcx kg traverse "MATCH (s:Service)-[:CALLS]->(d:Service) RETURN s, d" --with-insights
```

### Options

```
      --env string         Environment scope
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for traverse
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, table, yaml (default "table")
      --page int           Page number (0-based)
      --since string       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --with-insights      Include active SAAFE insights on each entity
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

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights

