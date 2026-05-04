## gcx kg cypher

Run a read-only Cypher query against the Knowledge Graph.

```
gcx kg cypher <query> [flags]
```

### Examples

```
  gcx kg cypher "MATCH (s:Service) RETURN s LIMIT 10"
  gcx kg cypher "MATCH (s:Service)-[:CALLS]->(d:Service) RETURN s, d" --since 1h
  gcx kg cypher "MATCH (s:Service {namespace: 'prod'}) RETURN s" --since 1h
```

### Options

```
      --from string     Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help            help for cypher
      --insights-only   Return only entities with active insights
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, table, yaml (default "table")
      --page int        Page number (0-based)
      --since string    Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --to string       End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights

