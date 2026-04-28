## gcx kg entities list

List Knowledge Graph entities for a given type.

```
gcx kg entities list [flags]
```

### Examples

```
  gcx kg entities list --type Service
  gcx kg entities list --type Service --namespace mimir-prod-01 --property name=model-builder
  gcx kg entities list --type Service --property name=~builder --insights-only
```

### Options

```
      --details                Include entity properties and insights in output
      --env string             Environment scope (run 'gcx kg describe scopes' to see valid values)
      --from string            Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                   help for list
      --insights-only          Only return entities with active insights
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int              Maximum number of items to return (0 for all) (default 50)
      --namespace string       Namespace scope (run 'gcx kg describe scopes' to see valid values)
  -o, --output string          Output format. One of: json, table, yaml (default "table")
      --page int               Page number (0-based)
      --property stringArray   Filter by property: name=value (exact) or name=~value (contains); repeatable (run 'gcx kg describe schema' to list property names)
      --since string           Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string            Site scope (run 'gcx kg describe scopes' to see valid values)
      --to string              End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string            Entity type to list (run 'gcx kg describe schema' to see available types)
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

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

