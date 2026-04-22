## gcx kg search entities

Search Knowledge Graph entities by name.

### Synopsis

Search Knowledge Graph entities whose names contain the query string.

Results are ranked by match quality: exact name matches first, then prefix
matches, then substring matches. Use --type to restrict to one entity type.
Scope flags (--env, --namespace, --site) are optional; omit them to search
across all scopes.

When no results are found, available scope values are printed as recovery hints.

```
gcx kg search entities <query> [flags]
```

### Examples

```
  # Search across all entity types
  gcx kg search entities api-server

  # Narrow to a specific type
  gcx kg search entities api-server --type Service

  # Filter to a specific environment
  gcx kg search entities api-server --env prod

  # JSON output for scripting
  gcx kg search entities api-server --output json
```

### Options

```
      --env string         Environment scope
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for entities
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int          Maximum results per entity type (default 20)
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, table, yaml (default "table")
      --since string       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string        Entity type (default: all types)
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

* [gcx kg search](gcx_kg_search.md)	 - Search Knowledge Graph entities or insights.

